package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/podhmo/go-classic-mpa-sample/internal/auth"
)

func TestCreateUserAndAuthenticate(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	hash, err := auth.HashPassword("correct horse")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	id, err := s.CreateUser(ctx, "alice", hash)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	u, err := s.UserByID(ctx, id)
	if err != nil {
		t.Fatalf("UserByID: %v", err)
	}
	if u.Name != "alice" {
		t.Errorf("name = %q, want alice", u.Name)
	}
	if _, err := s.UserByID(ctx, 999); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing user: err = %v, want ErrNotFound", err)
	}

	got, err := s.AuthenticateUser(ctx, "alice", "correct horse")
	if err != nil {
		t.Fatalf("AuthenticateUser: %v", err)
	}
	if got.ID != id {
		t.Errorf("authenticated ID = %d, want %d", got.ID, id)
	}
	// Login lookup follows the column's COLLATE NOCASE.
	if _, err := s.AuthenticateUser(ctx, "ALICE", "correct horse"); err != nil {
		t.Errorf("case-insensitive login failed: %v", err)
	}
	for _, tc := range []struct{ name, password string }{
		{"alice", "wrong"},
		{"nobody", "correct horse"},
	} {
		if _, err := s.AuthenticateUser(ctx, tc.name, tc.password); !errors.Is(err, ErrInvalidCredentials) {
			t.Errorf("AuthenticateUser(%q, …): err = %v, want ErrInvalidCredentials", tc.name, err)
		}
	}
}

func TestCreateUserDuplicateName(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	hash, _ := auth.HashPassword("pw")

	if _, err := s.CreateUser(ctx, "Bob", hash); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	for _, name := range []string{"Bob", "bob", "BOB"} {
		if _, err := s.CreateUser(ctx, name, hash); !errors.Is(err, ErrDuplicateName) {
			t.Errorf("CreateUser(%q): err = %v, want ErrDuplicateName", name, err)
		}
	}
}

func TestSessionLifecycle(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	hash, _ := auth.HashPassword("pw")
	uid, err := s.CreateUser(ctx, "carol", hash)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	token, err := s.CreateSession(ctx, uid, 30*24*time.Hour)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	// The database must hold only the token's hash, never the token itself.
	var rowCount int
	if err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM sessions WHERE id = ?", token).Scan(&rowCount); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if rowCount != 0 {
		t.Error("plaintext token must not be stored")
	}
	if err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM sessions WHERE id = ?", auth.HashToken(token)).Scan(&rowCount); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if rowCount != 1 {
		t.Error("expected the hashed token to be stored")
	}

	u, err := s.SessionUser(ctx, token)
	if err != nil {
		t.Fatalf("SessionUser: %v", err)
	}
	if u.ID != uid {
		t.Errorf("session user ID = %d, want %d", u.ID, uid)
	}

	if err := s.DeleteSession(ctx, token); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if _, err := s.SessionUser(ctx, token); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleted session: err = %v, want ErrNotFound", err)
	}
	if _, err := s.SessionUser(ctx, "no-such-token"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown session: err = %v, want ErrNotFound", err)
	}
}

func TestSessionExpiryAndRenewal(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	hash, _ := auth.HashPassword("pw")
	uid, _ := s.CreateUser(ctx, "dave", hash)
	insert := func(token string, createdAt, expiresAt time.Time) {
		t.Helper()
		if _, err := s.db.ExecContext(ctx,
			"INSERT INTO sessions(id, user_id, created_at, expires_at) VALUES(?, ?, ?, ?)",
			auth.HashToken(token), uid, createdAt.Unix(), expiresAt.Unix()); err != nil {
			t.Fatalf("insert session: %v", err)
		}
	}

	now := time.Now().UTC()

	// Expired sessions do not resolve and are swept on lookup.
	insert("expired", now.Add(-48*time.Hour), now.Add(-time.Hour))
	if _, err := s.SessionUser(ctx, "expired"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired session: err = %v, want ErrNotFound", err)
	}
	var n int
	s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sessions WHERE id = ?",
		auth.HashToken("expired")).Scan(&n)
	if n != 0 {
		t.Error("expired session should be deleted on lookup")
	}

	// Sliding renewal: past the half-life point, expires_at is pushed out
	// by the session's original lifetime (12h here).
	insert("stale", now.Add(-12*time.Hour), now.Add(-2*time.Hour))
	if _, err := s.SessionUser(ctx, "stale"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("setup assumption broken: stale session should be expired, got %v", err)
	}
	insert("renew", now.Add(-10*time.Hour), now.Add(2*time.Hour))
	if _, err := s.SessionUser(ctx, "renew"); err != nil {
		t.Fatalf("SessionUser: %v", err)
	}
	var expiresAt int64
	if err := s.db.QueryRowContext(ctx,
		"SELECT expires_at FROM sessions WHERE id = ?", auth.HashToken("renew")).Scan(&expiresAt); err != nil {
		t.Fatalf("read expires_at: %v", err)
	}
	want := time.Now().UTC().Add(12 * time.Hour)
	if dt := expiresAt - want.Unix(); dt < -60 || dt > 60 {
		t.Errorf("renewed expires_at drift = %ds, want ≈ +12h from now", dt)
	}

	// Fresh sessions (more than half the lifetime left) are not renewed.
	insert("fresh", now.Add(-time.Hour), now.Add(11*time.Hour))
	before := now.Add(11 * time.Hour).Unix()
	if _, err := s.SessionUser(ctx, "fresh"); err != nil {
		t.Fatalf("SessionUser: %v", err)
	}
	if err := s.db.QueryRowContext(ctx,
		"SELECT expires_at FROM sessions WHERE id = ?", auth.HashToken("fresh")).Scan(&expiresAt); err != nil {
		t.Fatalf("read expires_at: %v", err)
	}
	if expiresAt != before {
		t.Errorf("fresh session was renewed: expires_at = %d, want %d", expiresAt, before)
	}
}
