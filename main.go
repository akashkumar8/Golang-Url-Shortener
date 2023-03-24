package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/gorilla/mux"
)

const (
	DBUser     = "root"
	DBPassword = "akashdev@123"
	DBName     = "shorten_urls"
)

type URL struct {
	ID        int       `json:"id"`
	ShortLink string    `json:"short_link"`
	FullLink  string    `json:"full_link"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

func main() {
	router := mux.NewRouter()

	router.HandleFunc("/shorten", shortenLink).Methods("POST")
	router.HandleFunc("/{shortLink:[a-zA-Z0-9]{1,11}}", redirectToLink).Methods("GET")

	err := http.ListenAndServe(":8080", router)
	if err != nil {
		log.Fatal("Failed to start server: ", err)
	}
}

func shortenLink(w http.ResponseWriter, r *http.Request) {
	fullLink := r.FormValue("url")
	if fullLink == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "url parameter is required"})
		return
	}

	if len(fullLink) > 2048 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "url is too long"})
		return
	}

	db, err := sql.Open("mysql", fmt.Sprintf("%s:%s@/%s", DBUser, DBPassword, DBName))
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "failed to connect to database"})
		return
	}
	defer db.Close()

	err = db.Ping()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "failed to connect to database"})
		return
	}

	// Check if we have reached the maximum number of short links
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM urls").Scan(&count)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "failed to fetch URL count"})
		return
	}
	if count >= 20000 {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "service temporarily unavailable"})
		return
	}

	// Check if the URL already exists in the database
	var url URL
	err = db.QueryRow("SELECT id, short_link, full_link, created_at, expires_at FROM urls WHERE full_link = ?", fullLink).Scan(&url.ID, &url.ShortLink, &url.FullLink, &url.CreatedAt, &url.ExpiresAt)
	if err == nil {
		json.NewEncoder(w).Encode(url)
		return
	}

	// Generate a new short link
	shortLink := generateShortLink()
	for isShortLinkInUse(*db, shortLink) {
		shortLink = generateShortLink()
	}

	// Insert the new URL into the database
	expiresAt := time.Now().Add(24 * time.Hour)
	stmt, err := db.Prepare("INSERT INTO urls(short_link, full_link, created_at, expires_at) VALUES (?, ?, ?, ?)")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "failed to prepare database statement"})
		return
	}

	res, err := stmt.Exec(shortLink, fullLink, time.Now(), expiresAt)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "failed to insert URL into database"})
		return
	}

	id, err := res.LastInsertId()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "failed to get last insert ID"})
		return
	}

	url = URL{
		ID:        int(id),
		ShortLink: shortLink,
		FullLink:  fullLink,
		CreatedAt: time.Now(),
		ExpiresAt: expiresAt,
	}

	json.NewEncoder(w).Encode(url)
}

func generateShortLink() string {
	var letterRunes = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")
	b := make([]rune, 11)
	for i := range b {
		b[i] = letterRunes[rand.Intn(len(letterRunes))]
	}
	return string(b)
}

func isShortLinkInUse(db sql.DB, shortLink string) bool {
	var count int
	err := db.QueryRow("SELECT COUNT() FROM urls WHERE short_link = ?", shortLink).Scan(&count)
	if err != nil {
		return true
	}
	return count > 0
}

func redirectToLink(w http.ResponseWriter, r *http.Request) {
	shortLink := mux.Vars(r)["shortLink"]

	db, err := sql.Open("mysql", fmt.Sprintf("%s:%s@/%s", DBUser, DBPassword, DBName))
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "failed to connect to database"})
		return
	}
	defer db.Close()

	err = db.Ping()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "failed to connect to database"})
		return
	}

	var url URL
	err = db.QueryRow("SELECT id, short_link, full_link, created_at, expires_at FROM urls WHERE short_link = ?", shortLink).Scan(&url.ID, &url.ShortLink, &url.FullLink, &url.CreatedAt, &url.ExpiresAt)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "short link not found"})
		return
	}

	if time.Now().After(url.ExpiresAt) {
		w.WriteHeader(http.StatusGone)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "short link expired"})
		return
	}

	http.Redirect(w, r, url.FullLink, http.StatusSeeOther)
}
