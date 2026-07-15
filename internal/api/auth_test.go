package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lieyan/firescribe/internal/api"
	"github.com/lieyan/firescribe/internal/app"
	"github.com/lieyan/firescribe/internal/db"
	"github.com/lieyan/firescribe/internal/recognizer"
	"github.com/lieyan/firescribe/internal/storage"
	"github.com/lieyan/firescribe/internal/updater"
)

const (
	testAdminUsername = "admin"
	testAdminPassword = "admin-password-1"
)

// setupTestAdmin bootstraps the first administrator through the public setup
// endpoint and returns its session cookie.
func setupTestAdmin(t *testing.T, handler http.Handler) *http.Cookie {
	t.Helper()
	recorder := authRequest(handler, http.MethodPost, "/api/auth/setup", map[string]any{
		"username": testAdminUsername, "password": testAdminPassword,
	}, nil)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("setup admin status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	cookie := sessionCookie(recorder)
	if cookie == nil {
		t.Fatal("setup admin: session cookie missing")
	}
	return cookie
}

// authedHandler wraps handler so every request carries a fresh admin session.
// Existing feature tests use it to keep exercising the real auth middleware.
func authedHandler(t *testing.T, handler http.Handler) http.Handler {
	t.Helper()
	cookie := setupTestAdmin(t, handler)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.AddCookie(cookie)
		handler.ServeHTTP(w, r)
	})
}

func authRequest(handler http.Handler, method, path string, body any, cookie *http.Cookie) *httptest.ResponseRecorder {
	var reader *bytes.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder
}

func sessionCookie(recorder *httptest.ResponseRecorder) *http.Cookie {
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == "firescribe_session" && cookie.Value != "" {
			return cookie
		}
	}
	return nil
}

func decodeAuthResponse(t *testing.T, recorder *httptest.ResponseRecorder, target any) {
	t.Helper()
	if err := json.NewDecoder(recorder.Body).Decode(target); err != nil {
		t.Fatalf("decode response: %v (body = %s)", err, recorder.Body.String())
	}
}

func newAuthTestHandler(t *testing.T, updateRuntime ...api.UpdateRuntime) http.Handler {
	t.Helper()
	conn, err := db.Open(filepath.Join(t.TempDir(), "firescribe.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err := db.Migrate(conn); err != nil {
		t.Fatal(err)
	}
	files, err := storage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	application := app.New(app.NewStore(conn), files, recognizer.MockRecognizer{})
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		application.Shutdown(shutdownCtx)
	})
	return api.New(application, "", nil, updateRuntime...).Routes()
}

func TestAuthSetupAndForcedLogin(t *testing.T) {
	handler := newAuthTestHandler(t)

	status := authRequest(handler, http.MethodGet, "/api/auth/status", nil, nil)
	var statusBody struct {
		NeedsSetup    bool      `json:"needs_setup"`
		Authenticated bool      `json:"authenticated"`
		User          *app.User `json:"user"`
	}
	decodeAuthResponse(t, status, &statusBody)
	if !statusBody.NeedsSetup || statusBody.Authenticated {
		t.Fatalf("fresh status = %+v, want needs_setup && !authenticated", statusBody)
	}

	if got := authRequest(handler, http.MethodGet, "/api/documents", nil, nil); got.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous documents status = %d, want 401", got.Code)
	}
	if got := authRequest(handler, http.MethodGet, "/api/settings", nil, nil); got.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous settings status = %d, want 401", got.Code)
	}

	weak := authRequest(handler, http.MethodPost, "/api/auth/setup", map[string]any{"username": "admin", "password": "short"}, nil)
	if weak.Code != http.StatusBadRequest {
		t.Fatalf("weak password setup status = %d, want 400", weak.Code)
	}

	admin := setupTestAdmin(t, handler)

	again := authRequest(handler, http.MethodPost, "/api/auth/setup", map[string]any{"username": "other", "password": "another-pass-1"}, nil)
	if again.Code != http.StatusConflict {
		t.Fatalf("second setup status = %d, want 409", again.Code)
	}

	status = authRequest(handler, http.MethodGet, "/api/auth/status", nil, admin)
	decodeAuthResponse(t, status, &statusBody)
	if statusBody.NeedsSetup || !statusBody.Authenticated || statusBody.User == nil || statusBody.User.Role != app.RoleAdmin {
		t.Fatalf("post-setup status = %+v, want authenticated admin", statusBody)
	}

	if got := authRequest(handler, http.MethodGet, "/api/documents", nil, admin); got.Code != http.StatusOK {
		t.Fatalf("admin documents status = %d, body = %s", got.Code, got.Body.String())
	}

	logout := authRequest(handler, http.MethodPost, "/api/auth/logout", nil, admin)
	if logout.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d", logout.Code)
	}
	if got := authRequest(handler, http.MethodGet, "/api/documents", nil, admin); got.Code != http.StatusUnauthorized {
		t.Fatalf("documents after logout status = %d, want 401", got.Code)
	}

	wrong := authRequest(handler, http.MethodPost, "/api/auth/login", map[string]any{
		"username": testAdminUsername, "password": "wrong-password-1",
	}, nil)
	if wrong.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password login status = %d, want 401", wrong.Code)
	}

	login := authRequest(handler, http.MethodPost, "/api/auth/login", map[string]any{
		"username": strings.ToUpper(testAdminUsername), "password": testAdminPassword,
	}, nil)
	if login.Code != http.StatusOK || sessionCookie(login) == nil {
		t.Fatalf("login status = %d, body = %s", login.Code, login.Body.String())
	}
}

func TestUserManagementAndRoles(t *testing.T) {
	handler := newAuthTestHandler(t)
	admin := setupTestAdmin(t, handler)

	created := authRequest(handler, http.MethodPost, "/api/users", map[string]any{
		"username": "reviewer", "password": "reviewer-pass-1", "display_name": "校对员",
	}, admin)
	if created.Code != http.StatusCreated {
		t.Fatalf("create user status = %d, body = %s", created.Code, created.Body.String())
	}
	var reviewer app.User
	decodeAuthResponse(t, created, &reviewer)
	if reviewer.Role != app.RoleUser || reviewer.DisplayName != "校对员" {
		t.Fatalf("unexpected created user: %+v", reviewer)
	}

	duplicate := authRequest(handler, http.MethodPost, "/api/users", map[string]any{
		"username": "REVIEWER", "password": "reviewer-pass-2",
	}, admin)
	if duplicate.Code != http.StatusConflict {
		t.Fatalf("duplicate username status = %d, want 409", duplicate.Code)
	}

	login := authRequest(handler, http.MethodPost, "/api/auth/login", map[string]any{
		"username": "reviewer", "password": "reviewer-pass-1",
	}, nil)
	if login.Code != http.StatusOK {
		t.Fatalf("reviewer login status = %d, body = %s", login.Code, login.Body.String())
	}
	user := sessionCookie(login)

	// Regular users can use features but not management endpoints.
	if got := authRequest(handler, http.MethodGet, "/api/documents", nil, user); got.Code != http.StatusOK {
		t.Fatalf("user documents status = %d", got.Code)
	}
	if got := authRequest(handler, http.MethodGet, "/api/users", nil, user); got.Code != http.StatusForbidden {
		t.Fatalf("user list-users status = %d, want 403", got.Code)
	}
	if got := authRequest(handler, http.MethodGet, "/api/settings", nil, user); got.Code != http.StatusForbidden {
		t.Fatalf("user get-settings status = %d, want 403", got.Code)
	}
	if got := authRequest(handler, http.MethodPost, "/api/recognizer-profiles", map[string]any{}, user); got.Code != http.StatusForbidden {
		t.Fatalf("user create-profile status = %d, want 403", got.Code)
	}
	if got := authRequest(handler, http.MethodGet, "/api/recognizer-profiles", nil, user); got.Code != http.StatusOK {
		t.Fatalf("user list-profiles status = %d, want 200", got.Code)
	}
	if got := authRequest(handler, http.MethodPost, "/api/update/check", nil, user); got.Code != http.StatusForbidden {
		t.Fatalf("user update-check status = %d, want 403", got.Code)
	}

	// Last-admin protections.
	var adminUser app.User
	me := authRequest(handler, http.MethodGet, "/api/auth/status", nil, admin)
	var statusBody struct {
		User *app.User `json:"user"`
	}
	decodeAuthResponse(t, me, &statusBody)
	adminUser = *statusBody.User

	if got := authRequest(handler, http.MethodPatch, "/api/users/"+adminUser.ID, map[string]any{"role": "user"}, admin); got.Code != http.StatusConflict {
		t.Fatalf("demote last admin status = %d, want 409", got.Code)
	}
	if got := authRequest(handler, http.MethodPatch, "/api/users/"+adminUser.ID, map[string]any{"disabled": true}, admin); got.Code != http.StatusBadRequest {
		t.Fatalf("self-disable status = %d, want 400", got.Code)
	}
	if got := authRequest(handler, http.MethodDelete, "/api/users/"+adminUser.ID, nil, admin); got.Code != http.StatusBadRequest {
		t.Fatalf("self-delete status = %d, want 400", got.Code)
	}

	// Promote the reviewer, then the original admin may step down.
	if got := authRequest(handler, http.MethodPatch, "/api/users/"+reviewer.ID, map[string]any{"role": "admin"}, admin); got.Code != http.StatusOK {
		t.Fatalf("promote status = %d, body = %s", got.Code, got.Body.String())
	}
	if got := authRequest(handler, http.MethodPatch, "/api/users/"+adminUser.ID, map[string]any{"role": "user"}, admin); got.Code != http.StatusOK {
		t.Fatalf("demote with backup admin status = %d, body = %s", got.Code, got.Body.String())
	}
	// Role changes apply to live sessions immediately.
	if got := authRequest(handler, http.MethodGet, "/api/users", nil, admin); got.Code != http.StatusForbidden {
		t.Fatalf("demoted admin list-users status = %d, want 403", got.Code)
	}
	if got := authRequest(handler, http.MethodGet, "/api/users", nil, user); got.Code != http.StatusOK {
		t.Fatalf("promoted reviewer list-users status = %d, want 200", got.Code)
	}

	// Disabling revokes access at once and blocks new logins.
	if got := authRequest(handler, http.MethodPatch, "/api/users/"+adminUser.ID, map[string]any{"disabled": true}, user); got.Code != http.StatusOK {
		t.Fatalf("disable status = %d, body = %s", got.Code, got.Body.String())
	}
	if got := authRequest(handler, http.MethodGet, "/api/documents", nil, admin); got.Code != http.StatusUnauthorized {
		t.Fatalf("disabled account access status = %d, want 401", got.Code)
	}
	blocked := authRequest(handler, http.MethodPost, "/api/auth/login", map[string]any{
		"username": testAdminUsername, "password": testAdminPassword,
	}, nil)
	if blocked.Code != http.StatusUnauthorized {
		t.Fatalf("disabled login status = %d, want 401", blocked.Code)
	}

	// Admin password reset revokes the target's sessions.
	if got := authRequest(handler, http.MethodPatch, "/api/users/"+adminUser.ID, map[string]any{"disabled": false, "password": "rotated-pass-1"}, user); got.Code != http.StatusOK {
		t.Fatalf("reset password status = %d, body = %s", got.Code, got.Body.String())
	}
	rotated := authRequest(handler, http.MethodPost, "/api/auth/login", map[string]any{
		"username": testAdminUsername, "password": "rotated-pass-1",
	}, nil)
	if rotated.Code != http.StatusOK {
		t.Fatalf("login with rotated password status = %d, body = %s", rotated.Code, rotated.Body.String())
	}

	// Deleting a user removes the account.
	if got := authRequest(handler, http.MethodDelete, "/api/users/"+adminUser.ID, nil, user); got.Code != http.StatusNoContent {
		t.Fatalf("delete user status = %d, body = %s", got.Code, got.Body.String())
	}
	gone := authRequest(handler, http.MethodPost, "/api/auth/login", map[string]any{
		"username": testAdminUsername, "password": "rotated-pass-1",
	}, nil)
	if gone.Code != http.StatusUnauthorized {
		t.Fatalf("deleted account login status = %d, want 401", gone.Code)
	}
}

func TestChangeOwnPassword(t *testing.T) {
	handler := newAuthTestHandler(t)
	admin := setupTestAdmin(t, handler)

	wrong := authRequest(handler, http.MethodPost, "/api/auth/password", map[string]any{
		"current_password": "not-the-password", "new_password": "next-password-1",
	}, admin)
	if wrong.Code != http.StatusForbidden {
		t.Fatalf("wrong current password status = %d, want 403", wrong.Code)
	}

	changed := authRequest(handler, http.MethodPost, "/api/auth/password", map[string]any{
		"current_password": testAdminPassword, "new_password": "next-password-1",
	}, admin)
	if changed.Code != http.StatusOK {
		t.Fatalf("change password status = %d, body = %s", changed.Code, changed.Body.String())
	}
	// The session performing the change stays valid.
	if got := authRequest(handler, http.MethodGet, "/api/documents", nil, admin); got.Code != http.StatusOK {
		t.Fatalf("session after password change status = %d", got.Code)
	}
	relogin := authRequest(handler, http.MethodPost, "/api/auth/login", map[string]any{
		"username": testAdminUsername, "password": "next-password-1",
	}, nil)
	if relogin.Code != http.StatusOK {
		t.Fatalf("relogin status = %d, body = %s", relogin.Code, relogin.Body.String())
	}
}

func TestAdminTokenAutomationPaths(t *testing.T) {
	handler := newAuthTestHandler(t, api.UpdateRuntime{Config: updater.Config{AdminToken: "secret-token"}})
	setupTestAdmin(t, handler)

	// Management endpoints accept the configured token without a session; the
	// 503 proves the guard passed (settings runtime is nil in this test).
	req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	req.Header.Set("X-Admin-Token", "secret-token")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("token settings status = %d, want 503 (guard passed)", recorder.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	req.Header.Set("X-Admin-Token", "wrong-token")
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token settings status = %d, want 401", recorder.Code)
	}

	// Update endpoints keep the token contract for anonymous automation; the
	// 503 proves the guard passed (updater is nil in this test).
	req = httptest.NewRequest(http.MethodPost, "/api/update/check", nil)
	req.RemoteAddr = "192.0.2.10:12345"
	req.Header.Set("X-Admin-Token", "secret-token")
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("token update-check status = %d, want 503 (guard passed)", recorder.Code)
	}
}

func TestUpdateLoopbackWithoutTokenStillWorks(t *testing.T) {
	handler := newAuthTestHandler(t)
	setupTestAdmin(t, handler)

	req := httptest.NewRequest(http.MethodPost, "/api/update/check", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("loopback update-check status = %d, want 503 (guard passed)", recorder.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/update/check", nil)
	req.RemoteAddr = "192.0.2.10:12345"
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("remote update-check status = %d, want 403", recorder.Code)
	}
}
