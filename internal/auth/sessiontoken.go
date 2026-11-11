package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"time"
)

const defaultSessionTTL = time.Second * (60 * 60 * 24 * 7)

func GenerateSessionToken() string {
	key := make([]byte, 32)
	rand.Read(key)
	encodedStr := hex.EncodeToString(key)
	return encodedStr
}

func HashSessionToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

func SetSessionCookie(w http.ResponseWriter, token string) {
	cookie := &http.Cookie{
		Name:     "session_id",
		Value:    token,
		Path:     "/",
		MaxAge:   int(defaultSessionTTL),
		HttpOnly: true,
		Secure:   true, // Set to false for local development without HTTPS
		SameSite: http.SameSiteStrictMode,
	}
	http.SetCookie(w, cookie)
}

func ClearSessionCookie(w http.ResponseWriter) {
	cookie := &http.Cookie{
		Name:     "session_id",
		Value:    "",
		Path:     "/",
		MaxAge:   -1, // Delete immediately
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	}
	http.SetCookie(w, cookie)
}
