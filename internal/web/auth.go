// Account routes: register, login, logout. Authentication here is
// identification only — every page stays usable while logged out, and no
// handler below ever denies access for a missing session.
package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/podhmo/go-classic-mpa-sample/internal/auth"
	"github.com/podhmo/go-classic-mpa-sample/internal/store"
)

const (
	// maxNameRunes caps the account name (login name = display name).
	maxNameRunes = 100
	// minPasswordRunes is the only password complexity rule — a local app
	// needs no more (docs/sketch/ja/accounts.md).
	minPasswordRunes = 8
	// maxPasswordRunes bounds the input argon2 hashes server-side.
	maxPasswordRunes = 256
	// failedLoginDelay slows online brute-forcing and reduces the timing
	// gap between "no such user" and "wrong password". A real rate limit
	// is out of scope.
	failedLoginDelay = 200 * time.Millisecond
)

// authFormPage is the shared view model for the register and login forms.
type authFormPage struct {
	Title         string
	Heading       string
	Action        string
	SubmitLabel   string
	Name          string // preserved draft of the name field
	Next          string // sanitized ?next= target
	NewPassword   bool   // register → autocomplete="new-password"
	Error         string // top-level failure (bad credentials)
	NameError     string
	PasswordError string
	OtherLabel    string // footer link: "Log in" ↔ "Register"
	OtherURL      string
	Workspace     *store.Workspace
	Section       string
	User          *store.User
}

func (p authFormPage) anyError() bool {
	return p.Error != "" || p.NameError != "" || p.PasswordError != ""
}

// safeNext limits the post-login redirect to same-site paths; anything
// else falls back to /issues. Backslashes are rejected because browsers
// normalize "/\host" to "//host", which again resolves off-site.
func safeNext(raw string) string {
	if raw != "" && strings.HasPrefix(raw, "/") && !strings.HasPrefix(raw, "//") && !strings.ContainsRune(raw, '\\') {
		return raw
	}
	return "/issues"
}

// setSessionCookie issues the session cookie. Secure is deliberately
// absent — the app runs on plain-HTTP localhost (record it if TLS ever
// lands); HttpOnly + SameSite=Lax are the meaningful attributes here.
func setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

// clearSessionCookie expires the session cookie.
func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// registerForm renders GET /register; logged-in users are sent back.
func (h *handler) registerForm(w http.ResponseWriter, r *http.Request) {
	if userFromContext(r.Context()) != nil {
		http.Redirect(w, r, safeNext(r.URL.Query().Get("next")), http.StatusSeeOther)
		return
	}
	h.r.renderAuthForm(w, http.StatusOK, authFormPage{
		Title:       "Register",
		Heading:     "Create an account",
		Action:      "/register",
		SubmitLabel: "Register",
		Next:        safeNext(r.URL.Query().Get("next")),
		NewPassword: true,
		OtherLabel:  "Already have an account? Log in",
		OtherURL:    "/login",
	})
}

// register validates POST /register, creates the account and logs the new
// user in immediately.
func (h *handler) register(w http.ResponseWriter, r *http.Request) {
	if !h.parseForm(w, r) {
		return
	}
	name := strings.TrimSpace(r.PostFormValue("name"))
	password := r.PostFormValue("password")
	next := safeNext(r.PostFormValue("next"))

	page := authFormPage{
		Title:       "Register",
		Heading:     "Create an account",
		Action:      "/register",
		SubmitLabel: "Register",
		Name:        name,
		Next:        next,
		NewPassword: true,
		OtherLabel:  "Already have an account? Log in",
		OtherURL:    "/login",
	}
	switch {
	case name == "":
		page.NameError = "Name is required."
	case utf8.RuneCountInString(name) > maxNameRunes:
		page.NameError = fmt.Sprintf("Name must be %d characters or fewer.", maxNameRunes)
	}
	switch {
	case password == "":
		page.PasswordError = "Password is required."
	case utf8.RuneCountInString(password) < minPasswordRunes:
		page.PasswordError = fmt.Sprintf("Password must be %d characters or more.", minPasswordRunes)
	case utf8.RuneCountInString(password) > maxPasswordRunes:
		page.PasswordError = fmt.Sprintf("Password must be %d characters or fewer.", maxPasswordRunes)
	}
	if page.anyError() {
		h.r.renderAuthForm(w, http.StatusUnprocessableEntity, page)
		return
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	userID, err := h.store.CreateUser(r.Context(), name, hash)
	if errors.Is(err, store.ErrDuplicateName) {
		page.NameError = "That name is already taken."
		h.r.renderAuthForm(w, http.StatusUnprocessableEntity, page)
		return
	}
	if err != nil {
		h.internalError(w, r, err)
		return
	}

	// Registration logs the user in: issue a fresh session immediately.
	token, err := h.store.CreateSession(r.Context(), userID, sessionTTL)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	setSessionCookie(w, token)
	http.Redirect(w, r, next, http.StatusSeeOther)
}

// loginForm renders GET /login; logged-in users are sent back.
func (h *handler) loginForm(w http.ResponseWriter, r *http.Request) {
	if userFromContext(r.Context()) != nil {
		http.Redirect(w, r, safeNext(r.URL.Query().Get("next")), http.StatusSeeOther)
		return
	}
	h.r.renderAuthForm(w, http.StatusOK, authFormPage{
		Title:       "Log in",
		Heading:     "Log in",
		Action:      "/login",
		SubmitLabel: "Log in",
		Next:        safeNext(r.URL.Query().Get("next")),
		OtherLabel:  "No account yet? Register",
		OtherURL:    "/register",
	})
}

// login validates POST /login: on success it issues a fresh session token
// (the old cookie value is simply unreferenced — session fixation needs a
// pre-existing anonymous session, which this app never creates).
func (h *handler) login(w http.ResponseWriter, r *http.Request) {
	if !h.parseForm(w, r) {
		return
	}
	name := strings.TrimSpace(r.PostFormValue("name"))
	password := r.PostFormValue("password")
	next := safeNext(r.PostFormValue("next"))

	page := authFormPage{
		Title:       "Log in",
		Heading:     "Log in",
		Action:      "/login",
		SubmitLabel: "Log in",
		Name:        name,
		Next:        next,
		OtherLabel:  "No account yet? Register",
		OtherURL:    "/register",
	}
	if name == "" {
		page.NameError = "Name is required."
	}
	if password == "" {
		page.PasswordError = "Password is required."
	}
	if page.anyError() {
		h.r.renderAuthForm(w, http.StatusUnprocessableEntity, page)
		return
	}

	user, err := h.store.AuthenticateUser(r.Context(), name, password)
	if errors.Is(err, store.ErrInvalidCredentials) {
		time.Sleep(failedLoginDelay)
		// Deliberately vague: never reveal which of name/password failed.
		page.Error = "Name or password is incorrect."
		h.r.renderAuthForm(w, http.StatusUnprocessableEntity, page)
		return
	}
	if err != nil {
		h.internalError(w, r, err)
		return
	}

	token, err := h.store.CreateSession(r.Context(), user.ID, sessionTTL)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	setSessionCookie(w, token)
	http.Redirect(w, r, next, http.StatusSeeOther)
}

// logout handles POST /logout: delete the session row and expire the
// cookie. It is a POST on purpose — a GET logout could be triggered by a
// prefetch or an <img> on a foreign page.
func (h *handler) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookieName); err == nil {
		if err := h.store.DeleteSession(r.Context(), c.Value); err != nil {
			h.internalError(w, r, err)
			return
		}
	}
	clearSessionCookie(w)
	http.Redirect(w, r, "/issues", http.StatusSeeOther)
}

// sessionCookieName is the cookie carrying the opaque session token.
const sessionCookieName = "session"

// sessionTTL is the server-side session lifetime. The cookie itself is a
// browser-session cookie (no Max-Age): the cap lives in the sessions table
// and renews slidingly while the browser stays open, so closing the
// browser still drops the session.
const sessionTTL = 30 * 24 * time.Hour

type contextKey int

const userContextKey contextKey = iota

// userFromContext returns the logged-in user resolved by withUser, or nil
// for an anonymous request.
func userFromContext(ctx context.Context) *store.User {
	u, _ := ctx.Value(userContextKey).(*store.User)
	return u
}

// authorIDFromContext maps the logged-in user to the author_id recorded on
// new posts; 0 means anonymous.
func authorIDFromContext(ctx context.Context) int64 {
	if u := userFromContext(ctx); u != nil {
		return u.ID
	}
	return 0
}

// withUser is the authentication middleware: it resolves the session
// cookie to a *store.User on the request context. Anonymous access is
// fully supported — a nil user simply means "not logged in", and only an
// unknown/expired session resolves that way. A lookup that fails for any
// other reason (e.g. a transient DB error) is a 500 instead: proceeding
// anonymously there would silently record an authored post with no
// author.
func withUser(h *handler, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie(sessionCookieName); err == nil {
			u, err := h.store.SessionUser(r.Context(), c.Value, sessionTTL)
			switch {
			case err == nil:
				r = r.WithContext(context.WithValue(r.Context(), userContextKey, u))
			case errors.Is(err, store.ErrNotFound):
				// Unknown/expired session: continue anonymously.
			default:
				h.renderError(w, r, http.StatusInternalServerError,
					"Internal server error", "The session could not be verified. Please try again.")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
