package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5/middleware"
)

func TestRequireUpdateTokenLoopbackUsesTCPPeer(t *testing.T) {
	s := &Server{}
	handler := capturePeerAddr(middleware.RealIP(s.requireUpdateToken(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})))

	tests := []struct {
		name       string
		remoteAddr string
		forwarded  string
		wantStatus int
	}{
		{name: "IPv4 loopback", remoteAddr: "127.0.0.1:12345", wantStatus: http.StatusNoContent},
		{name: "IPv6 loopback", remoteAddr: "[::1]:12345", wantStatus: http.StatusNoContent},
		{name: "spoofed XFF cannot turn remote peer into loopback", remoteAddr: "192.0.2.10:12345", forwarded: "127.0.0.1", wantStatus: http.StatusForbidden},
		{name: "XFF cannot turn loopback peer into remote", remoteAddr: "127.0.0.1:12345", forwarded: "203.0.113.10", wantStatus: http.StatusNoContent},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/update/check", nil)
			req.RemoteAddr = tt.remoteAddr
			if tt.forwarded != "" {
				req.Header.Set("X-Forwarded-For", tt.forwarded)
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)
			if recorder.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, tt.wantStatus, recorder.Body.String())
			}
		})
	}
}

func TestRequireUpdateTokenHeaders(t *testing.T) {
	s := &Server{}
	s.updateCfg.AdminToken = "secret-token"
	handler := capturePeerAddr(s.requireUpdateToken(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	tests := []struct {
		name       string
		headers    map[string]string
		wantStatus int
	}{
		{name: "missing", wantStatus: http.StatusUnauthorized},
		{name: "wrong", headers: map[string]string{"X-Admin-Token": "wrong"}, wantStatus: http.StatusUnauthorized},
		{name: "custom header", headers: map[string]string{"X-Admin-Token": "secret-token"}, wantStatus: http.StatusNoContent},
		{name: "bearer", headers: map[string]string{"Authorization": "Bearer secret-token"}, wantStatus: http.StatusNoContent},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/update/apply", nil)
			req.RemoteAddr = "192.0.2.10:12345"
			for key, value := range tt.headers {
				req.Header.Set(key, value)
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)
			if recorder.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, tt.wantStatus, recorder.Body.String())
			}
		})
	}
}
