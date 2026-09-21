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
	return strings.TrimSpace(os.Getenv("RENDER")) != "" || strings.TrimSpace(os.Getenv("RENDER_SERVICE_ID")) != ""
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
		return errors.New("Render 部署必须设置 JUKU_BASIC_AUTH_USER 和 JUKU_BASIC_AUTH_PASS")
	}
	return nil
}

func hostedSecretEqual(left, right string) bool {
	leftHash := sha256.Sum256([]byte(left))
	rightHash := sha256.Sum256([]byte(right))
	return subtle.ConstantTimeCompare(leftHash[:], rightHash[:]) == 1
}

func hostedAuthExempt(path string) bool {
	if path == "/healthz" || path == tvboxAPIPath || path == tvboxAPIPathNoSlash || path == tvboxConfigPath || path == tvboxCoverPath || path == tvboxPlayPath {
		return true
	}
	if strings.HasPrefix(path, "/api/emby/media/") {
		return true
	}
	switch path {
	case "/api/emby/stream.m3u8", "/api/emby/segment.ts", "/api/emby/cover", "/api/emby/merged.mp4":
		return true
	default:
		return false
	}
}

func wrapHostedBasicAuth(next http.Handler) http.Handler {
	username := strings.TrimSpace(os.Getenv("JUKU_BASIC_AUTH_USER"))
	password := os.Getenv("JUKU_BASIC_AUTH_PASS")
	if username == "" || password == "" {
		return next
	}
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if hostedAuthExempt(request.URL.Path) {
			next.ServeHTTP(writer, request)
			return
		}
		providedUser, providedPassword, ok := request.BasicAuth()
		if !ok || !hostedSecretEqual(providedUser, username) || !hostedSecretEqual(providedPassword, password) {
			writer.Header().Set("WWW-Authenticate", `Basic realm="juku", charset="UTF-8"`)
			writer.Header().Set("Cache-Control", "no-store")
			http.Error(writer, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(writer, request)
	})
}

func handleHealthz(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		writer.Header().Set("Allow", "GET, HEAD")
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
	writer.WriteHeader(http.StatusOK)
	if request.Method == http.MethodGet {
		_, _ = writer.Write([]byte("ok"))
	}
}
