package api

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/lieyan/firescribe/internal/app"
	"github.com/lieyan/firescribe/internal/auth"
)

const (
	sessionCookieName      = "firescribe_session"
	sessionTTL             = 30 * 24 * time.Hour
	sessionRefreshInterval = 15 * time.Minute
	minPasswordLength      = 8
	maxPasswordLength      = 128
)

var usernamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{2,31}$`)

type sessionContextKey struct{}

type sessionInfo struct {
	user      app.User
	tokenHash string
}

func currentSession(r *http.Request) (sessionInfo, bool) {
	session, ok := r.Context().Value(sessionContextKey{}).(sessionInfo)
	return session, ok
}

// withUser resolves the session cookie into the request context. It never
// rejects requests: requireAuth/requireAdmin decide what each route needs.
func (s *Server) withUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookieName)
		if err != nil || strings.TrimSpace(cookie.Value) == "" {
			next.ServeHTTP(w, r)
			return
		}
		tokenHash := auth.HashSessionToken(cookie.Value)
		user, lastSeen, err := s.app.Store.GetSessionUser(r.Context(), tokenHash)
		if err != nil {
			if !errors.Is(err, sql.ErrNoRows) {
				writeError(w, err)
				return
			}
			clearSessionCookie(w, r)
			next.ServeHTTP(w, r)
			return
		}
		if time.Since(lastSeen) > sessionRefreshInterval {
			// Sliding expiry: an account in regular use never logs itself out.
			if err := s.app.Store.RefreshAuthSession(r.Context(), tokenHash, time.Now().Add(sessionTTL)); err != nil {
				writeError(w, err)
				return
			}
		}
		ctx := context.WithValue(r.Context(), sessionContextKey{}, sessionInfo{user: user, tokenHash: tokenHash})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := currentSession(r); !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requireAdmin guards management endpoints: a signed-in administrator, or the
// configured update.admin_token for headless automation. Unlike the update
// endpoints there is no loopback fallback — settings stay behind login even on
// localhost.
func (s *Server) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if session, ok := currentSession(r); ok {
			if session.user.IsAdmin() {
				next(w, r)
				return
			}
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "administrator role required"})
			return
		}
		if s.validAdminToken(r) {
			next(w, r)
			return
		}
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
	}
}

// requireUpdateAccess keeps the pre-login OTA contract for anonymous
// automation (admin token, or loopback when no token is configured) while
// letting signed-in administrators manage updates from the web UI.
func (s *Server) requireUpdateAccess(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if session, ok := currentSession(r); ok {
			if session.user.IsAdmin() {
				next(w, r)
				return
			}
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "administrator role required"})
			return
		}
		s.requireUpdateToken(next)(w, r)
	}
}

func (s *Server) validAdminToken(r *http.Request) bool {
	token := strings.TrimSpace(s.updateCfg.AdminToken)
	if token == "" {
		return false
	}
	provided := strings.TrimSpace(r.Header.Get("X-Admin-Token"))
	if provided == "" {
		if header := r.Header.Get("Authorization"); strings.HasPrefix(header, "Bearer ") {
			provided = strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
		}
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(token)) == 1
}

func requestIsSecure(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func setSessionCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(sessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   requestIsSecure(r),
		SameSite: http.SameSiteLaxMode,
	})
}

func clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   requestIsSecure(r),
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) startSession(w http.ResponseWriter, r *http.Request, userID string) error {
	token, tokenHash, err := auth.NewSessionToken()
	if err != nil {
		return err
	}
	if err := s.app.Store.CreateAuthSession(r.Context(), tokenHash, userID, time.Now().Add(sessionTTL)); err != nil {
		return err
	}
	setSessionCookie(w, r, token)
	return nil
}

func normalizeUsername(username string) (string, error) {
	username = strings.ToLower(strings.TrimSpace(username))
	if !usernamePattern.MatchString(username) {
		return "", errors.New("username must be 3-32 characters of letters, digits, '.', '_' or '-' and start with a letter or digit")
	}
	return username, nil
}

func validatePassword(password string) error {
	if len(password) < minPasswordLength {
		return errors.New("password must be at least 8 characters")
	}
	if len(password) > maxPasswordLength {
		return errors.New("password must be at most 128 characters")
	}
	return nil
}

func decodeAuthBody(w http.ResponseWriter, r *http.Request, target any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(target); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return false
	}
	return true
}

func (s *Server) authStatus(w http.ResponseWriter, r *http.Request) {
	count, err := s.app.Store.CountUsers(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	response := struct {
		NeedsSetup    bool      `json:"needs_setup"`
		Authenticated bool      `json:"authenticated"`
		User          *app.User `json:"user,omitempty"`
	}{NeedsSetup: count == 0}
	if session, ok := currentSession(r); ok {
		response.Authenticated = true
		response.User = &session.user
	}
	writeJSON(w, http.StatusOK, response)
}

// authSetup creates the very first administrator account. Once any account
// exists the endpoint is closed for good.
func (s *Server) authSetup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username    string `json:"username"`
		Password    string `json:"password"`
		DisplayName string `json:"display_name"`
	}
	if !decodeAuthBody(w, r, &req) {
		return
	}
	count, err := s.app.Store.CountUsers(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	if count > 0 {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "initial setup is already complete"})
		return
	}
	username, err := normalizeUsername(req.Username)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := validatePassword(req.Password); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	displayName := strings.TrimSpace(req.DisplayName)
	if displayName == "" {
		displayName = username
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeError(w, err)
		return
	}
	user, err := s.app.Store.CreateUser(r.Context(), username, displayName, app.RoleAdmin, hash, true)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.startSession(w, r, user.ID); err != nil {
		writeError(w, err)
		return
	}
	_ = s.app.Store.TouchUserLogin(r.Context(), user.ID)
	writeJSON(w, http.StatusCreated, user)
}

func (s *Server) authLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decodeAuthBody(w, r, &req) {
		return
	}
	writeInvalidCredentials := func() {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid username or password"})
	}
	username := strings.ToLower(strings.TrimSpace(req.Username))
	if username == "" || req.Password == "" {
		auth.FakeVerify(req.Password)
		writeInvalidCredentials()
		return
	}
	user, hash, err := s.app.Store.GetUserCredentials(r.Context(), username)
	if errors.Is(err, sql.ErrNoRows) {
		auth.FakeVerify(req.Password)
		writeInvalidCredentials()
		return
	}
	if err != nil {
		writeError(w, err)
		return
	}
	if !auth.VerifyPassword(hash, req.Password) {
		writeInvalidCredentials()
		return
	}
	if user.Disabled {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "account is disabled"})
		return
	}
	_ = s.app.Store.PurgeExpiredAuthSessions(r.Context())
	if err := s.startSession(w, r, user.ID); err != nil {
		writeError(w, err)
		return
	}
	if err := s.app.Store.TouchUserLogin(r.Context(), user.ID); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, user)
}

func (s *Server) authLogout(w http.ResponseWriter, r *http.Request) {
	if session, ok := currentSession(r); ok {
		if err := s.app.Store.DeleteAuthSession(r.Context(), session.tokenHash); err != nil {
			writeError(w, err)
			return
		}
	}
	clearSessionCookie(w, r)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) authChangePassword(w http.ResponseWriter, r *http.Request) {
	session, ok := currentSession(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
		return
	}
	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if !decodeAuthBody(w, r, &req) {
		return
	}
	if err := validatePassword(req.NewPassword); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	hash, err := s.app.Store.GetUserPasswordHash(r.Context(), session.user.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	if !auth.VerifyPassword(hash, req.CurrentPassword) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "current password is incorrect"})
		return
	}
	nextHash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.app.Store.SetUserPassword(r.Context(), session.user.ID, nextHash); err != nil {
		writeError(w, err)
		return
	}
	// Changing the password signs out every other device.
	if err := s.app.Store.DeleteUserAuthSessions(r.Context(), session.user.ID, session.tokenHash); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "password_changed"})
}

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.app.Store.ListUsers(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, users)
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username    string `json:"username"`
		Password    string `json:"password"`
		DisplayName string `json:"display_name"`
		Role        string `json:"role"`
	}
	if !decodeAuthBody(w, r, &req) {
		return
	}
	username, err := normalizeUsername(req.Username)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := validatePassword(req.Password); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	role := req.Role
	if strings.TrimSpace(role) == "" {
		role = app.RoleUser
	}
	role, err = app.ValidateRole(role)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	displayName := strings.TrimSpace(req.DisplayName)
	if displayName == "" {
		displayName = username
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeError(w, err)
		return
	}
	user, err := s.app.Store.CreateUser(r.Context(), username, displayName, role, hash, false)
	if errors.Is(err, app.ErrUsernameTaken) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "username is already taken"})
		return
	}
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, user)
}

func (s *Server) patchUser(w http.ResponseWriter, r *http.Request) {
	target, err := s.app.Store.GetUser(r.Context(), chi.URLParam(r, "userID"))
	if err != nil {
		writeError(w, err)
		return
	}
	var req struct {
		DisplayName *string `json:"display_name"`
		Role        *string `json:"role"`
		Disabled    *bool   `json:"disabled"`
		Password    *string `json:"password"`
	}
	if !decodeAuthBody(w, r, &req) {
		return
	}
	session, hasSession := currentSession(r)
	isSelf := hasSession && session.user.ID == target.ID

	if req.Role != nil {
		role, err := app.ValidateRole(*req.Role)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		req.Role = &role
	}
	if req.DisplayName != nil {
		trimmed := strings.TrimSpace(*req.DisplayName)
		if trimmed == "" {
			trimmed = target.Username
		}
		req.DisplayName = &trimmed
	}
	if req.Password != nil {
		if err := validatePassword(*req.Password); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
	}
	if isSelf && req.Disabled != nil && *req.Disabled {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "cannot disable your own account"})
		return
	}

	demotes := req.Role != nil && *req.Role != app.RoleAdmin && target.IsAdmin()
	disables := req.Disabled != nil && *req.Disabled && !target.Disabled
	if target.IsAdmin() && !target.Disabled && (demotes || disables) {
		others, err := s.app.Store.CountOtherActiveAdmins(r.Context(), target.ID)
		if err != nil {
			writeError(w, err)
			return
		}
		if others == 0 {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "at least one enabled administrator is required"})
			return
		}
	}

	updated, err := s.app.Store.PatchUser(r.Context(), target.ID, req.DisplayName, req.Role, req.Disabled)
	if err != nil {
		writeError(w, err)
		return
	}
	if req.Password != nil {
		hash, err := auth.HashPassword(*req.Password)
		if err != nil {
			writeError(w, err)
			return
		}
		if err := s.app.Store.SetUserPassword(r.Context(), target.ID, hash); err != nil {
			writeError(w, err)
			return
		}
		keep := ""
		if isSelf {
			keep = session.tokenHash
		}
		if err := s.app.Store.DeleteUserAuthSessions(r.Context(), target.ID, keep); err != nil {
			writeError(w, err)
			return
		}
	}
	if req.Disabled != nil && *req.Disabled {
		if err := s.app.Store.DeleteUserAuthSessions(r.Context(), target.ID, ""); err != nil {
			writeError(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) deleteUser(w http.ResponseWriter, r *http.Request) {
	target, err := s.app.Store.GetUser(r.Context(), chi.URLParam(r, "userID"))
	if err != nil {
		writeError(w, err)
		return
	}
	if session, ok := currentSession(r); ok && session.user.ID == target.ID {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "cannot delete your own account"})
		return
	}
	if target.IsAdmin() && !target.Disabled {
		others, err := s.app.Store.CountOtherActiveAdmins(r.Context(), target.ID)
		if err != nil {
			writeError(w, err)
			return
		}
		if others == 0 {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "at least one enabled administrator is required"})
			return
		}
	}
	if err := s.app.Store.DeleteUser(r.Context(), target.ID); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
