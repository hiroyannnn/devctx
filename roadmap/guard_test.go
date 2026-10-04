package roadmap

import (
	"net"
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

func TestHandler_RequestGuardPort80AllowsHostWithoutPort(t *testing.T) {
	// ブラウザは :80 を Host から省く
	h := (&Server{Port: 80}).Handler()
	for host, want := range map[string]int{
		"127.0.0.1":    http.StatusOK,
		"localhost":    http.StatusOK,
		"127.0.0.1:80": http.StatusOK,
		"evil.com":     http.StatusForbidden,
	} {
		req := httptest.NewRequest("GET", "/", nil)
		req.Host = host
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != want {
			t.Errorf("host %q: status = %d, want %d", host, w.Code, want)
		}
	}
	// Origin も Host と同じ形（ポートなし）で一致すれば通る
	req := httptest.NewRequest("POST", "/api/islands", strings.NewReader("{}"))
	req.Host = "localhost"
	req.Header.Set("Origin", "http://localhost")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST: status = %d", w.Code)
	}
	// :80 でない port では、ポートなしの Host は許可しない
	req = httptest.NewRequest("GET", "/", nil)
	req.Host = "localhost"
	w = httptest.NewRecorder()
	(&Server{Port: 3333}).Handler().ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("port 3333 without port: status = %d", w.Code)
	}
}

func TestServe_PortZeroUsesTheActualListeningPort(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &Server{Port: 0}
	go srv.Serve(ln)
	defer ln.Close()

	_, port, _ := net.SplitHostPort(ln.Addr().String())
	get := func(host string) int {
		req, _ := http.NewRequest("GET", "http://"+ln.Addr().String()+"/", nil)
		req.Host = host
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	if got := get("127.0.0.1:" + port); got != http.StatusOK {
		t.Errorf("actual port: status = %d", got)
	}
	if got := get("localhost:" + port); got != http.StatusOK {
		t.Errorf("localhost: status = %d", got)
	}
	if got := get("127.0.0.1:0"); got != http.StatusForbidden {
		t.Errorf("port 0 host: status = %d", got)
	}
	if got := get("evil.com"); got != http.StatusForbidden {
		t.Errorf("evil host: status = %d", got)
	}
}
