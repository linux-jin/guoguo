package app

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"net/http"
	"os"
	"strings"
)

func hostedPlatform() bool {
	return os.Getenv("RENDER") != "" || os.Getenv("RENDER_SERVICE_ID") != ""
}

func applyHostedListenDefaults(listen *string, open *bool) {
	if !flagWasSet("listen") {
		if port := strings.TrimSpace(os.Getenv("PORT")); port != "" {
			*listen = "0.0.0.0:" + port
		}
	}
	if !flagWasSet("open") && hostedPlatform() {
		*open = false
	}
}

func requireHostedAuth() error {
	if !hostedPlatform() {
		return nil
	}
	if strings.TrimSpace(os.Getenv("JUKU_BASIC_AUTH_USER")) == "" || os.Getenv("JUKU_BASIC_AUTH_PASS") == "" {
		return errors.New("hosted platforms require JUKU_BASIC_AUTH_USER and JUKU_BASIC_AUTH_PASS")
	}
	return nil
}

func secretEqual(a, b string) bool {
	sumA := sha256.Sum256([]byte(a))
	sumB := sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(sumA[:], sumB[:]) == 1
}

func wrapBasicAuth(next http.Handler) http.Handler {
	user := strings.TrimSpace(os.Getenv("JUKU_BASIC_AUTH_USER"))
	pass := os.Getenv("JUKU_BASIC_AUTH_PASS")
	if user == "" || pass == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		gotUser, gotPass, ok := r.BasicAuth()
		if !ok || !secretEqual(gotUser, user) || !secretEqual(gotPass, pass) {
			w.Header().Set("WWW-Authenticate", `Basic realm="juku"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}
