package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

type App struct {
	mu    sync.RWMutex
	links map[string]string
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
	if err != nil || target.Host == "" ||
		(target.Scheme != "http" && target.Scheme != "https") {
		http.Error(w, "URL must start with http:// or https://", http.StatusBadRequest)
		return
	}

	var code string

	for {
		bytes := make([]byte, 6)

		if _, err := rand.Read(bytes); err != nil {
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		code = base64.RawURLEncoding.EncodeToString(bytes)

		a.mu.Lock()
		_, exists := a.links[code]

		if !exists {
			a.links[code] = target.String()
		}
		a.mu.Unlock()

		if !exists {
			break
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)

	_ = json.NewEncoder(w).Encode(map[string]string{
		"code":         code,
		"original_url": target.String(),
		"short_url":    "http://" + r.Host + "/" + code,
	})
}

func (a *App) redirect(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")

	a.mu.RLock()
	target, ok := a.links[code]
	a.mu.RUnlock()

	if !ok {
		http.NotFound(w, r)
		return
	}

	http.Redirect(w, r, target, http.StatusFound)
}

func main() {
	app := &App{
		links: make(map[string]string),
	}

	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		_ = json.NewEncoder(w).Encode(map[string]string{
			"status": "ok",
		})
	})

	mux.HandleFunc("POST /links", app.createLink)
	mux.HandleFunc("GET /{code}", app.redirect)

	log.Println("URL Shortener running at http://localhost:8081")

	log.Fatal(http.ListenAndServe(":8081", mux))
}
