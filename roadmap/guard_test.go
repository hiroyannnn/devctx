package roadmap

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandler_RequestGuard(t *testing.T) {
	srv := &Server{Port: 3333}
	tests := []struct {
		name        string
		method      string
		host        string
		origin      string // "-" は Origin ヘッダなし
		contentType string
		want        int
	}{
		{"GET 127.0.0.1", "GET", "127.0.0.1:3333", "-", "", http.StatusOK},
		{"GET localhost", "GET", "localhost:3333", "-", "", http.StatusOK},
		{"GET wrong host (DNS rebinding)", "GET", "evil.com", "-", "", http.StatusForbidden},
		{"GET wrong host with port", "GET", "evil.com:3333", "-", "", http.StatusForbidden},
		{"GET wrong port", "GET", "127.0.0.1:4444", "-", "", http.StatusForbidden},
		{"GET host without port", "GET", "127.0.0.1", "-", "", http.StatusForbidden},
		{"GET ipv6 loopback is not allowed", "GET", "[::1]:3333", "-", "", http.StatusForbidden},
		{"HEAD ok", "HEAD", "localhost:3333", "-", "", http.StatusOK},
		{"POST ok 127", "POST", "127.0.0.1:3333", "http://127.0.0.1:3333", "application/json", http.StatusMethodNotAllowed},
		{"POST ok localhost origin+host", "POST", "localhost:3333", "http://localhost:3333", "application/json", http.StatusMethodNotAllowed},
		{"POST json with charset", "POST", "localhost:3333", "http://localhost:3333", "application/json; charset=utf-8", http.StatusMethodNotAllowed},
		{"POST missing origin", "POST", "127.0.0.1:3333", "-", "application/json", http.StatusForbidden},
		{"POST null origin", "POST", "127.0.0.1:3333", "null", "application/json", http.StatusForbidden},
		{"POST cross origin", "POST", "127.0.0.1:3333", "http://evil.com", "application/json", http.StatusForbidden},
		{"POST 127 origin with localhost host", "POST", "localhost:3333", "http://127.0.0.1:3333", "application/json", http.StatusForbidden},
		{"POST https origin", "POST", "127.0.0.1:3333", "https://127.0.0.1:3333", "application/json", http.StatusForbidden},
		{"POST wrong content type", "POST", "127.0.0.1:3333", "http://127.0.0.1:3333", "text/plain", http.StatusUnsupportedMediaType},
		{"POST form content type", "POST", "127.0.0.1:3333", "http://127.0.0.1:3333", "application/x-www-form-urlencoded", http.StatusUnsupportedMediaType},
		{"POST no content type", "POST", "127.0.0.1:3333", "http://127.0.0.1:3333", "", http.StatusUnsupportedMediaType},
		{"DELETE needs origin too", "DELETE", "127.0.0.1:3333", "-", "application/json", http.StatusForbidden},
		{"POST wrong host even with matching origin", "POST", "evil.com", "http://evil.com", "application/json", http.StatusForbidden},
	}
	h := srv.Handler()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := "/"
			if tt.method != "GET" && tt.method != "HEAD" {
				path = "/api/islands" // 通れば GET 専用 handler の 405 に当たる
			}
			req := httptest.NewRequest(tt.method, path, strings.NewReader("{}"))
			req.Host = tt.host
			if tt.origin != "-" {
				req.Header.Set("Origin", tt.origin)
			}
			if tt.contentType != "" {
				req.Header.Set("Content-Type", tt.contentType)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			if w.Code != tt.want {
				t.Errorf("status = %d, want %d (body %q)", w.Code, tt.want, w.Body.String())
			}
		})
	}
}
