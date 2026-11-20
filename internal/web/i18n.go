package web

// Prototype localization (docs/sketch/ja/i18n.md): two locales, "en" default
// and "ja", resolved per request into the context and bound into templates
// as {{t "key"}} / {{lang}} at parse time.

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"net/http"
	"path"
	"strings"
)

//go:embed i18n/*.json
var i18nFS embed.FS

// supportedLangs lists every locale with a catalog file. The first entry is
// the fallback: a missing key in another catalog resolves to it, and it is
// what unrecognized requests get.
var supportedLangs = []string{"en", "ja"}

// langCookieName persists the explicit choice made via ?lang=<code>.
const langCookieName = "lang"

// catalogs maps locale → key → printf-style message.
var catalogs = map[string]map[string]string{}

func init() {
	for _, lang := range supportedLangs {
		var m map[string]string
		b, err := i18nFS.ReadFile("i18n/" + lang + ".json")
		if err != nil {
			panic(fmt.Sprintf("i18n: %v", err))
		}
		if err := json.Unmarshal(b, &m); err != nil {
			panic(fmt.Sprintf("i18n %s: %v", lang, err))
		}
		catalogs[lang] = m
	}
}

// T returns a printf-style message lookup bound to lang, for use in both
// templates (as {{t}}) and handlers. Missing keys fall back to the default
// locale, then to the key itself so an untranslated spot stays visible.
func T(lang string) func(key string, args ...any) string {
	return func(key string, args ...any) string {
		if msg, ok := catalogs[lang][key]; ok {
			return fmt.Sprintf(msg, args...)
		}
		if msg, ok := catalogs[supportedLangs[0]][key]; ok {
			return fmt.Sprintf(msg, args...)
		}
		return key
	}
}

// localeContextKey: one struct type per context key (see auth.go).
type localeContextKey struct{}

// localeFromContext reports the locale withLocale stored on the request.
// Requests that never passed the middleware (tests, fragments rendered
// without it) default to the first supported language.
func localeFromContext(ctx context.Context) string {
	if lang, ok := ctx.Value(localeContextKey{}).(string); ok {
		return lang
	}
	return supportedLangs[0]
}

// tr translates key in the request's locale, for handler-side strings
// (page titles, headings) that are passed to templates via the view model.
func tr(r *http.Request, key string, args ...any) string {
	return T(localeFromContext(r.Context()))(key, args...)
}

// withLocale resolves the display language for the request. An explicit
// ?lang=<code> persists to a cookie and redirects to the same URL without
// the parameter (303, so every method ends on a clean GET); otherwise the
// cookie wins, then Accept-Language, then the default locale.
func withLocale(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if q := r.URL.Query().Get("lang"); validLang(q) {
			http.SetCookie(w, &http.Cookie{
				Name:     langCookieName,
				Value:    q,
				Path:     "/",
				MaxAge:   365 * 24 * 3600,
				SameSite: http.SameSiteLaxMode,
			})
			v := r.URL.Query()
			v.Del("lang")
			// Build the Location from a cleaned path: a path like
			// "//evil.example/x" survives RequestURI as a scheme-relative
			// Location header, which is an open redirect.
			target := path.Clean("/" + r.URL.Path)
			if q := v.Encode(); q != "" {
				target += "?" + q
			}
			http.Redirect(w, r, target, http.StatusSeeOther)
			return
		}
		lang := supportedLangs[0]
		if c, err := r.Cookie(langCookieName); err == nil && validLang(c.Value) {
			lang = c.Value
		} else if wantsJapanese(r.Header.Get("Accept-Language")) {
			lang = "ja"
		}
		r = r.WithContext(context.WithValue(r.Context(), localeContextKey{}, lang))
		next.ServeHTTP(w, r)
	})
}

func validLang(lang string) bool {
	for _, l := range supportedLangs {
		if l == lang {
			return true
		}
	}
	return false
}

// wantsJapanese reports whether Accept-Language prefers Japanese over
// English. Deliberately minimal: for a real multi-locale deployment use
// golang.org/x/text/language.ParseAcceptLanguage, which weighs q-values.
func wantsJapanese(header string) bool {
	for part := range strings.SplitSeq(header, ",") {
		tag, _, _ := strings.Cut(strings.TrimSpace(part), ";")
		if strings.HasPrefix(tag, "ja") {
			return true
		}
	}
	return false
}
