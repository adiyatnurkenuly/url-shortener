package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

type App struct {
	db *sql.DB
}

func (a *App) createLink(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	var input struct {
		URL string `json:"url"`
	}
	if err := decoder.Decode(&input); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		http.Error(w, "Only one JSON object is allowed", http.StatusBadRequest)
		return
	}

	target, err := url.Parse(strings.TrimSpace(input.URL))
	if err != nil || target.Hostname() == "" || target.User != nil ||
		(target.Scheme != "http" && target.Scheme != "https") {
		http.Error(w, "Enter a valid http:// or https:// URL", http.StatusBadRequest)
		return
	}

	for attempt := 0; attempt < 5; attempt++ {
		var randomBytes [6]byte
		if _, err := rand.Read(randomBytes[:]); err != nil {
			log.Println(err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		code := base64.RawURLEncoding.EncodeToString(randomBytes[:])

		var insertedCode string
		err := a.db.QueryRowContext(
			r.Context(),
			`INSERT INTO links (code, original_url)
			 VALUES ($1, $2)
			 ON CONFLICT (code) DO NOTHING
			 RETURNING code`,
			code,
			target.String(),
		).Scan(&insertedCode)

		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			log.Println(err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"code":         insertedCode,
			"original_url": target.String(),
			"short_url":    "http://" + r.Host + "/" + insertedCode,
		})
		return
	}

	http.Error(w, "Could not create a unique short link", http.StatusInternalServerError)
}

func (a *App) redirect(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	var originalURL string

	err := a.db.QueryRowContext(
		r.Context(),
		`UPDATE links
		 SET clicks = clicks + 1
		 WHERE code = $1
		 RETURNING original_url`,
		code,
	).Scan(&originalURL)

	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		log.Println(err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, originalURL, http.StatusFound)
}

func main() {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		log.Fatal("DATABASE_URL is not set")
	}

	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		log.Fatal("PostgreSQL connection failed: ", err)
	}
	log.Println("Connected to PostgreSQL")

	app := &App{db: db}
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status": "ok",
		})
	})

	mux.HandleFunc("POST /links", app.createLink)
	mux.HandleFunc("GET /{code}", app.redirect)

	server := &http.Server{
		Addr:              ":8081",
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Println("URL Shortener running at http://localhost:8081")
	log.Fatal(server.ListenAndServe())
}
