package main

import (
	"database/sql/driver"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

type validCode struct{}

func (validCode) Match(v driver.Value) bool {
	s, ok := v.(string)
	if !ok || len(s) != 8 {
		return false
	}
	b, err := base64.RawURLEncoding.DecodeString(s)
	return err == nil && len(b) == 6
}

func newTestApp(t *testing.T) (*http.ServeMux, sqlmock.Sqlmock) {
	t.Helper()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		mock.ExpectClose()

		if err := db.Close(); err != nil {
			t.Error(err)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
	})

	app := &App{db: db}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /links", app.createLink)
	mux.HandleFunc("GET /links/{code}/stats", app.getStats)
	mux.HandleFunc("DELETE /links/{code}", app.deleteLink)
	mux.HandleFunc("GET /{code}", app.redirect)

	return mux, mock
}

func TestCreateLink(t *testing.T) {
	mux, mock := newTestApp(t)

	mock.ExpectQuery("INSERT INTO links").
		WithArgs(validCode{}, "https://example.com").
		WillReturnRows(sqlmock.NewRows([]string{"code"}).AddRow("AbC123xy"))

	req := httptest.NewRequest(
		http.MethodPost, "http://localhost:8081/links",
		strings.NewReader(`{"url":"https://example.com"}`),
	)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
	}

	var response struct {
		Code        string `json:"code"`
		OriginalURL string `json:"original_url"`
		ShortURL    string `json:"short_url"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Code != "AbC123xy" ||
		response.OriginalURL != "https://example.com" ||
		response.ShortURL != "http://localhost:8081/AbC123xy" {
		t.Fatalf("unexpected response: %+v", response)
	}
}

func TestCreateLinkInvalidURL(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"Empty URL", `{"url":""}`},
		{"Missing scheme", `{"url":"example.com"}`},
		{"Wrong scheme", `{"url":"ftp://example.com"}`},
		{"Credentials in URL", `{"url":"https://user:pass@example.com"}`},
		{"Unknown JSON field", `{"url":"https://example.com","extra":1}`},
		{"Two JSON objects", `{"url":"https://example.com"}{"url":"https://other.com"}`},
		{"Malformed JSON", `{"url":`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux, _ := newTestApp(t)

			req := httptest.NewRequest(
				http.MethodPost, "/links", strings.NewReader(tt.body),
			)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("got %d, want 400: %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestCreateLinkRetriesCollision(t *testing.T) {
	mux, mock := newTestApp(t)

	mock.ExpectQuery("INSERT INTO links").
		WithArgs(validCode{}, "https://example.com").
		WillReturnRows(sqlmock.NewRows([]string{"code"}))

	mock.ExpectQuery("INSERT INTO links").
		WithArgs(validCode{}, "https://example.com").
		WillReturnRows(sqlmock.NewRows([]string{"code"}).AddRow("AbC123xy"))

	req := httptest.NewRequest(
		http.MethodPost, "/links",
		strings.NewReader(`{"url":"https://example.com"}`),
	)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("got %d, want 201: %s", rec.Code, rec.Body.String())
	}
}

func TestRedirect(t *testing.T) {
	for _, tt := range []struct {
		name   string
		found  bool
		status int
	}{
		{"Existing link", true, http.StatusFound},
		{"Missing link", false, http.StatusNotFound},
	} {
		t.Run(tt.name, func(t *testing.T) {
			mux, mock := newTestApp(t)

			rows := sqlmock.NewRows([]string{"original_url"})
			if tt.found {
				rows.AddRow("https://example.com")
			}

			mock.ExpectQuery("UPDATE links").
				WithArgs("abc123").
				WillReturnRows(rows)

			req := httptest.NewRequest(http.MethodGet, "/abc123", nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if rec.Code != tt.status {
				t.Fatalf("got %d, want %d", rec.Code, tt.status)
			}
			if tt.found && rec.Header().Get("Location") != "https://example.com" {
				t.Fatalf("wrong redirect: %s", rec.Header().Get("Location"))
			}
		})
	}
}

func TestGetStats(t *testing.T) {
	mux, mock := newTestApp(t)
	created := time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)

	mock.ExpectQuery("SELECT original_url, clicks, created_at").
		WithArgs("abc123").
		WillReturnRows(
			sqlmock.NewRows([]string{"original_url", "clicks", "created_at"}).
				AddRow("https://example.com", int64(3), created),
		)

	req := httptest.NewRequest(http.MethodGet, "/links/abc123/stats", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
	}

	var response struct {
		Code        string    `json:"code"`
		OriginalURL string    `json:"original_url"`
		Clicks      int64     `json:"clicks"`
		CreatedAt   time.Time `json:"created_at"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Code != "abc123" || response.Clicks != 3 ||
		response.OriginalURL != "https://example.com" ||
		!response.CreatedAt.Equal(created) {
		t.Fatalf("unexpected stats: %+v", response)
	}
}

func TestGetStatsNotFound(t *testing.T) {
	mux, mock := newTestApp(t)
	mock.ExpectQuery("SELECT original_url, clicks, created_at").
		WithArgs("missing").
		WillReturnRows(sqlmock.NewRows([]string{"original_url", "clicks", "created_at"}))

	req := httptest.NewRequest(http.MethodGet, "/links/missing/stats", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("got %d, want 404", rec.Code)
	}
}

func TestDeleteLink(t *testing.T) {
	for _, tt := range []struct {
		name     string
		affected int64
		status   int
	}{
		{"Existing link", 1, http.StatusNoContent},
		{"Missing link", 0, http.StatusNotFound},
	} {
		t.Run(tt.name, func(t *testing.T) {
			mux, mock := newTestApp(t)

			mock.ExpectExec("DELETE FROM links").
				WithArgs("abc123").
				WillReturnResult(sqlmock.NewResult(0, tt.affected))

			req := httptest.NewRequest(http.MethodDelete, "/links/abc123", nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if rec.Code != tt.status {
				t.Fatalf("got %d, want %d", rec.Code, tt.status)
			}
		})
	}
}
