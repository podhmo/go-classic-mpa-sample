package web

// Prototype localization (docs/sketch/ja/i18n.md): two locales, "en" default
// and "ja", resolved per request into the context and bound into templates
// as one function per message field — {{AppName}}, {{IssuesTitle}},
// {{IssuesCount .RangeStart .RangeEnd .Total}} — at parse time.
//
// The catalog is a Go struct, not JSON: field names are compile-time
// identifiers, so a typo'd field cannot exist, and a typo'd template call
// is a parse error ("function not defined"). JSON keys would be silent on
// both sides.

import (
	"context"
	"fmt"
	"html/template"
	"net/http"
	"path"
	"reflect"
	"strings"
)

// supportedLangs lists every locale with a messages value below. The first
// entry is the fallback: what unrecognized requests get, and what
// executeLang falls back to.
var supportedLangs = []string{"en", "ja"}

// langCookieName persists the explicit choice made via ?lang=<code>.
const langCookieName = "lang"

// messages is the whole UI-string catalog for one locale: one field per
// user-facing string. printf-format strings keep their verbs and take the
// args noted in the comment (templates pass positionals — a locale whose
// phrasing needs a different order uses %[n]d).
type messages struct {
	AppName string

	LayoutSkip             string
	LayoutNavMain          string
	LayoutNavIssues        string
	LayoutNavDiscussions   string
	LayoutNavLabels        string
	LayoutNavPulls         string
	LayoutNavWorkspaces    string
	LayoutNavNewIssue      string
	LayoutNavNewDiscussion string
	LayoutNavLogout        string
	LayoutNavLogin         string
	LayoutNavRegister      string
	LayoutFooter           string

	IssuesTitle           string
	IssuesTabsAria        string
	IssuesTabOpen         string
	IssuesTabClosed       string
	IssuesTabAll          string
	IssuesFilterAria      string
	IssuesFilterSearch    string
	IssuesFilterSearchPh  string
	IssuesFilterLabel     string
	IssuesFilterAllLabels string
	IssuesFilterSort      string
	IssuesSortNewest      string
	IssuesSortOldest      string
	IssuesFilterApply     string
	IssuesFilterClear     string
	IssuesCount           string // args: range start, range end, total
	IssuesStatusOpen      string
	IssuesStatusClosed    string
	IssuesOpened          string // args: formatted time
	IssuesPagerAria       string
	IssuesPagerPrev       string
	IssuesPagerNext       string
	IssuesPagerInfo       string // args: page, total pages
	IssuesEmptyFiltered   string
	IssuesEmptyTry        string
	IssuesEmptyClearLink  string
	IssuesEmptyNone       string
	IssuesEmptyCreate     string
}

var messagesEN = messages{
	AppName: "Issue Tracker",

	LayoutSkip:             "Skip to main content",
	LayoutNavMain:          "Main",
	LayoutNavIssues:        "Issues",
	LayoutNavDiscussions:   "Discussions",
	LayoutNavLabels:        "Labels",
	LayoutNavPulls:         "Pull requests",
	LayoutNavWorkspaces:    "Workspaces",
	LayoutNavNewIssue:      "New issue",
	LayoutNavNewDiscussion: "New discussion",
	LayoutNavLogout:        "Log out",
	LayoutNavLogin:         "Log in",
	LayoutNavRegister:      "Register",
	LayoutFooter:           "A classic multi-page app built with Go's net/http and html/template. Declarative first; minimal JavaScript where it earns its place.",

	IssuesTitle:           "Issues",
	IssuesTabsAria:        "Filter by status",
	IssuesTabOpen:         "Open",
	IssuesTabClosed:       "Closed",
	IssuesTabAll:          "All",
	IssuesFilterAria:      "Filter issues",
	IssuesFilterSearch:    "Search",
	IssuesFilterSearchPh:  "Title or body",
	IssuesFilterLabel:     "Label",
	IssuesFilterAllLabels: "All labels",
	IssuesFilterSort:      "Sort",
	IssuesSortNewest:      "Newest first",
	IssuesSortOldest:      "Oldest first",
	IssuesFilterApply:     "Apply",
	IssuesFilterClear:     "Clear",
	IssuesCount:           "Showing %[1]d–%[2]d of %[3]d issues",
	IssuesStatusOpen:      "Open",
	IssuesStatusClosed:    "Closed",
	IssuesOpened:          "opened %s",
	IssuesPagerAria:       "Pagination",
	IssuesPagerPrev:       "Previous",
	IssuesPagerNext:       "Next",
	IssuesPagerInfo:       "Page %d of %d",
	IssuesEmptyFiltered:   "No issues match the current filters.",
	IssuesEmptyTry:        "Try a different keyword or",
	IssuesEmptyClearLink:  "clear the filters",
	IssuesEmptyNone:       "No issues here yet.",
	IssuesEmptyCreate:     "Create the first issue",
}

var messagesJA = messages{
	AppName: "Issue Tracker",

	LayoutSkip:             "メインコンテンツへスキップ",
	LayoutNavMain:          "メイン",
	LayoutNavIssues:        "イシュー",
	LayoutNavDiscussions:   "ディスカッション",
	LayoutNavLabels:        "ラベル",
	LayoutNavPulls:         "プルリクエスト",
	LayoutNavWorkspaces:    "ワークスペース",
	LayoutNavNewIssue:      "新規イシュー",
	LayoutNavNewDiscussion: "新規ディスカッション",
	LayoutNavLogout:        "ログアウト",
	LayoutNavLogin:         "ログイン",
	LayoutNavRegister:      "登録",
	LayoutFooter:           "Go の net/http と html/template で作ったクラシックなマルチページアプリ。宣言的ファースト、必要なところにだけ最小限の JavaScript。",

	IssuesTitle:           "イシュー",
	IssuesTabsAria:        "ステータスで絞り込み",
	IssuesTabOpen:         "オープン",
	IssuesTabClosed:       "クローズ",
	IssuesTabAll:          "すべて",
	IssuesFilterAria:      "イシューを絞り込み",
	IssuesFilterSearch:    "検索",
	IssuesFilterSearchPh:  "タイトルまたは本文",
	IssuesFilterLabel:     "ラベル",
	IssuesFilterAllLabels: "すべてのラベル",
	IssuesFilterSort:      "並び替え",
	IssuesSortNewest:      "新しい順",
	IssuesSortOldest:      "古い順",
	IssuesFilterApply:     "適用",
	IssuesFilterClear:     "クリア",
	IssuesCount:           "%[3]d 件中 %[1]d–%[2]d 件を表示",
	IssuesStatusOpen:      "オープン",
	IssuesStatusClosed:    "クローズ",
	IssuesOpened:          "%s にオープン",
	IssuesPagerAria:       "ページネーション",
	IssuesPagerPrev:       "前へ",
	IssuesPagerNext:       "次へ",
	IssuesPagerInfo:       "%d / %d ページ",
	IssuesEmptyFiltered:   "現在のフィルタに一致するイシューはありません。",
	IssuesEmptyTry:        "別のキーワードを試すか、",
	IssuesEmptyClearLink:  "フィルタをクリア",
	IssuesEmptyNone:       "まだイシューはありません。",
	IssuesEmptyCreate:     "最初のイシューを作成",
}

// messagesFor resolves the catalog for a locale; anything unrecognized —
// including a bogus lang cookie — gets the default locale.
func messagesFor(lang string) *messages {
	switch lang {
	case "ja":
		return &messagesJA
	default:
		return &messagesEN
	}
}

// messageFuncs registers every field of the locale's messages struct as a
// template function named after the field ({{AppName}}, {{IssuesTitle}},
// {{IssuesCount args...}}). Values are printf format strings: args are only
// applied when the template passes them, so literal text needs no escaping.
func messageFuncs(funcs template.FuncMap, lang string) {
	v := reflect.ValueOf(*messagesFor(lang))
	fallback := reflect.ValueOf(messagesEN)
	typ := v.Type()
	for i := range typ.NumField() {
		msg := v.Field(i).String()
		if msg == "" {
			// A field not yet translated renders the en text, so a
			// half-filled ja catalog degrades instead of going blank.
			msg = fallback.Field(i).String()
		}
		funcs[typ.Field(i).Name] = func(args ...any) string {
			if len(args) == 0 {
				return msg
			}
			return fmt.Sprintf(msg, args...)
		}
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

// msgFor returns the catalog for the request's locale, for handler-side
// strings (page titles, headings) passed to templates via the view model.
func msgFor(r *http.Request) *messages {
	return messagesFor(localeFromContext(r.Context()))
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
