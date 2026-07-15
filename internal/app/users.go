package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	RoleAdmin = "admin"
	RoleUser  = "user"
)

var ErrUsernameTaken = errors.New("username is already taken")

type User struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Role        string `json:"role"`
	Disabled    bool   `json:"disabled"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
	LastLoginAt string `json:"last_login_at,omitempty"`
}

func (u User) IsAdmin() bool {
	return u.Role == RoleAdmin
}

const userColumns = `id, username, display_name, role, disabled, created_at, updated_at, COALESCE(last_login_at, '')`

func scanUser(row interface{ Scan(...any) error }) (User, error) {
	var user User
	err := row.Scan(&user.ID, &user.Username, &user.DisplayName, &user.Role, &user.Disabled, &user.CreatedAt, &user.UpdatedAt, &user.LastLoginAt)
	return user, err
}

func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&count)
	return count, err
}

// CountOtherActiveAdmins reports how many enabled administrators exist besides
// excludeID. Guards against demoting, disabling or deleting the last admin.
func (s *Store) CountOtherActiveAdmins(ctx context.Context, excludeID string) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM users
		WHERE role = ? AND disabled = 0 AND id != ?
	`, RoleAdmin, excludeID).Scan(&count)
	return count, err
}

// CreateUser inserts a new account. When firstAdmin is true the insert only
// succeeds while the users table is still empty, making initial setup safe
// against concurrent requests.
func (s *Store) CreateUser(ctx context.Context, username, displayName, role, passwordHash string, firstAdmin bool) (User, error) {
	timestamp := now()
	user := User{
		ID:          newID("usr"),
		Username:    username,
		DisplayName: displayName,
		Role:        role,
		CreatedAt:   timestamp,
		UpdatedAt:   timestamp,
	}
	query := `INSERT INTO users (id, username, display_name, role, disabled, password_hash, created_at, updated_at)
		VALUES (?, ?, ?, ?, 0, ?, ?, ?)`
	args := []any{user.ID, user.Username, user.DisplayName, user.Role, passwordHash, user.CreatedAt, user.UpdatedAt}
	if firstAdmin {
		query = `INSERT INTO users (id, username, display_name, role, disabled, password_hash, created_at, updated_at)
			SELECT ?, ?, ?, ?, 0, ?, ?, ? WHERE NOT EXISTS (SELECT 1 FROM users)`
	}
	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique constraint") {
			return User{}, ErrUsernameTaken
		}
		return User{}, err
	}
	if affected, err := result.RowsAffected(); err == nil && affected == 0 {
		return User{}, errors.New("initial account already exists")
	}
	return user, nil
}

func (s *Store) GetUser(ctx context.Context, id string) (User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE id = ?`, id))
}

// GetUserCredentials returns the account and its password hash for login.
func (s *Store) GetUserCredentials(ctx context.Context, username string) (User, string, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT `+userColumns+`, password_hash FROM users WHERE username = ?
	`, username)
	var user User
	var hash string
	err := row.Scan(&user.ID, &user.Username, &user.DisplayName, &user.Role, &user.Disabled, &user.CreatedAt, &user.UpdatedAt, &user.LastLoginAt, &hash)
	return user, hash, err
}

func (s *Store) GetUserPasswordHash(ctx context.Context, id string) (string, error) {
	var hash string
	err := s.db.QueryRowContext(ctx, `SELECT password_hash FROM users WHERE id = ?`, id).Scan(&hash)
	return hash, err
}

func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+userColumns+` FROM users ORDER BY created_at ASC, username ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	users := []User{}
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	return users, rows.Err()
}

// PatchUser applies partial profile updates. Password changes go through
// SetUserPassword so session invalidation stays explicit at the call site.
func (s *Store) PatchUser(ctx context.Context, id string, displayName, role *string, disabled *bool) (User, error) {
	sets := []string{"updated_at = ?"}
	args := []any{now()}
	if displayName != nil {
		sets = append(sets, "display_name = ?")
		args = append(args, *displayName)
	}
	if role != nil {
		sets = append(sets, "role = ?")
		args = append(args, *role)
	}
	if disabled != nil {
		sets = append(sets, "disabled = ?")
		args = append(args, *disabled)
	}
	args = append(args, id)
	result, err := s.db.ExecContext(ctx, `UPDATE users SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...)
	if err != nil {
		return User{}, err
	}
	if affected, err := result.RowsAffected(); err == nil && affected == 0 {
		return User{}, sql.ErrNoRows
	}
	return s.GetUser(ctx, id)
}

func (s *Store) SetUserPassword(ctx context.Context, id, passwordHash string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE users SET password_hash = ?, updated_at = ? WHERE id = ?`, passwordHash, now(), id)
	if err != nil {
		return err
	}
	if affected, err := result.RowsAffected(); err == nil && affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) DeleteUser(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if affected, err := result.RowsAffected(); err == nil && affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) TouchUserLogin(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET last_login_at = ? WHERE id = ?`, now(), id)
	return err
}

func (s *Store) CreateAuthSession(ctx context.Context, tokenHash, userID string, expiresAt time.Time) error {
	timestamp := now()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO auth_sessions (token_hash, user_id, created_at, expires_at, last_seen_at)
		VALUES (?, ?, ?, ?, ?)
	`, tokenHash, userID, timestamp, expiresAt.UTC().Format(time.RFC3339Nano), timestamp)
	return err
}

// GetSessionUser resolves an unexpired session to its enabled account and
// reports when the session was last refreshed.
func (s *Store) GetSessionUser(ctx context.Context, tokenHash string) (User, time.Time, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT u.id, u.username, u.display_name, u.role, u.disabled, u.created_at, u.updated_at, COALESCE(u.last_login_at, ''), s.last_seen_at
		FROM auth_sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = ? AND s.expires_at > ? AND u.disabled = 0
	`, tokenHash, now())
	var user User
	var lastSeen string
	err := row.Scan(&user.ID, &user.Username, &user.DisplayName, &user.Role, &user.Disabled, &user.CreatedAt, &user.UpdatedAt, &user.LastLoginAt, &lastSeen)
	if err != nil {
		return User{}, time.Time{}, err
	}
	seenAt, parseErr := time.Parse(time.RFC3339Nano, lastSeen)
	if parseErr != nil {
		seenAt = time.Time{}
	}
	return user, seenAt, nil
}

func (s *Store) RefreshAuthSession(ctx context.Context, tokenHash string, expiresAt time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE auth_sessions SET expires_at = ?, last_seen_at = ? WHERE token_hash = ?
	`, expiresAt.UTC().Format(time.RFC3339Nano), now(), tokenHash)
	return err
}

func (s *Store) DeleteAuthSession(ctx context.Context, tokenHash string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM auth_sessions WHERE token_hash = ?`, tokenHash)
	return err
}

// DeleteUserAuthSessions revokes a user's sessions. exceptTokenHash may name a
// session to keep (e.g. the one performing a password change); pass "" to
// revoke everything.
func (s *Store) DeleteUserAuthSessions(ctx context.Context, userID, exceptTokenHash string) error {
	var err error
	if exceptTokenHash == "" {
		_, err = s.db.ExecContext(ctx, `DELETE FROM auth_sessions WHERE user_id = ?`, userID)
	} else {
		_, err = s.db.ExecContext(ctx, `DELETE FROM auth_sessions WHERE user_id = ? AND token_hash != ?`, userID, exceptTokenHash)
	}
	return err
}

func (s *Store) PurgeExpiredAuthSessions(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM auth_sessions WHERE expires_at <= ?`, now())
	return err
}

// ValidateRole normalizes and checks a role value coming from the API.
func ValidateRole(role string) (string, error) {
	role = strings.ToLower(strings.TrimSpace(role))
	switch role {
	case RoleAdmin, RoleUser:
		return role, nil
	default:
		return "", fmt.Errorf("role must be %q or %q", RoleAdmin, RoleUser)
	}
}
