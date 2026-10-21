// Package util holds small shared helpers.
package util

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
)

// RandHex returns 2*n random hex characters.
func RandHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// RandToken returns an opaque URL-safe token with n random bytes.
func RandToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func Hash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func HashEqual(hash, s string) bool {
	return subtle.ConstantTimeCompare([]byte(hash), []byte(Hash(s))) == 1
}

// PKCEChallenge computes the S256 code challenge for a verifier.
func PKCEChallenge(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// SpotifyError writes an error in the Web API format.
func SpotifyError(w http.ResponseWriter, status int, msg string) {
	WriteJSON(w, status, map[string]any{"error": map[string]any{"status": status, "message": msg}})
}

func IsLoopbackHost(h string) bool { return h == "127.0.0.1" || h == "::1" }

// MatchRedirect mirrors Spotify's rule: exact match, except that a registered
// loopback URI without a port matches any port.
func MatchRedirect(registered []string, got string) bool {
	gu, err := url.Parse(got)
	if err != nil {
		return false
	}
	for _, r := range registered {
		if r == got {
			return true
		}
		ru, err := url.Parse(r)
		if err != nil {
			continue
		}
		if ru.Scheme == "http" && gu.Scheme == "http" && IsLoopbackHost(ru.Hostname()) &&
			ru.Hostname() == gu.Hostname() && ru.Port() == "" && ru.Path == gu.Path && ru.RawQuery == gu.RawQuery {
			return true
		}
	}
	return false
}

// ValidRedirect checks a redirect URI a virtual app wants to register.
func ValidRedirect(s string) bool {
	u, err := url.Parse(s)
	if err != nil || u.Scheme == "" || u.Fragment != "" {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "javascript", "data", "file", "vbscript", "blob", "about":
		return false
	case "https":
		return u.Host != ""
	case "http":
		return IsLoopbackHost(u.Hostname())
	}
	return true // custom schemes for native apps
}
