package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// testSessionCookie bootstraps the first administrator through the public
// setup endpoint and returns the session cookie for authenticated requests.
func testSessionCookie(t *testing.T, handler http.Handler) *http.Cookie {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/setup",
		strings.NewReader(`{"username":"admin","password":"admin-password-1"}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("setup admin status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == sessionCookieName && cookie.Value != "" {
			return cookie
		}
	}
	t.Fatal("setup admin: session cookie missing")
	return nil
}
