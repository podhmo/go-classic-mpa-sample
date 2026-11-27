package web

// UI localization (docs/sketch/ja/i18n.md): two locales, "en" default and
// "ja", resolved per request into the context and bound into templates as
// one function per message field — {{AppName}}, {{IssuesTitle}},
// {{IssuesCount .RangeStart .RangeEnd .Total}} — at parse time.
//
// The catalog is a Go struct, not JSON: field names are compile-time
// identifiers, so a typo'd field cannot exist, and a typo'd template call
// is a parse error ("function not defined"). JSON keys would be silent on
// both sides.

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"math"
	"net/http"
	"net/url"
	"path"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/podhmo/go-classic-mpa-sample/internal/store"
)

// supportedLangs lists every locale with a messages value below. The first
// entry is the fallback: what unrecognized requests get, and what
// executeLang falls back to.
var supportedLangs = []string{"en", "ja"}

// langCookieName persists the explicit choice made via ?lang=<code>.
const langCookieName = "lang"

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
// Fields ending in HTML return template.HTML with every arg escaped — the
// message itself is trusted markup, user data is not.
func messageFuncs(funcs template.FuncMap, lang string) {
	v := reflect.ValueOf(*messagesFor(lang))
	fallback := reflect.ValueOf(messagesEN)
	typ := v.Type()
	for i := range typ.NumField() {
		name := typ.Field(i).Name
		msg := v.Field(i).String()
		if msg == "" {
			// A field not yet translated renders the en text, so a
			// half-filled ja catalog degrades instead of going blank.
			msg = fallback.Field(i).String()
		}
		if strings.HasSuffix(name, "HTML") {
			funcs[name] = func(args ...any) template.HTML {
				if len(args) == 0 {
					return template.HTML(msg)
				}
				esc := make([]any, len(args))
				for j, a := range args {
					if s, ok := a.(string); ok {
						esc[j] = template.HTMLEscapeString(s)
					} else {
						esc[j] = a
					}
				}
				return template.HTML(fmt.Sprintf(msg, esc...))
			}
			continue
		}
		funcs[name] = func(args ...any) string {
			if len(args) == 0 {
				return msg
			}
			return fmt.Sprintf(msg, args...)
		}
	}

	// Locale-bound helpers shared with funcMap names: fmtTime and
	// authorName vary by locale, so parsePages overrides them after
	// cloning the shared map.
	funcs["fmtTime"] = func(t time.Time) string { return fmtTimeFor(lang, t) }
	funcs["authorName"] = func(u *store.User) string { return authorNameFor(lang, u) }
	funcs["hideReasonLabel"] = func(code string) string { return hideReasonLabelFor(lang, code) }
	funcs["noun"] = func(key string) string { return nounFor(lang, key) }
	funcs["nounTitle"] = func(key string) string { return nounTitleFor(lang, key) }
	funcs["repliesClause"] = func(n int) string { return repliesClauseFor(lang, n) }
	funcs["labelKind"] = func(kind string) string { return labelKindFor(lang, kind) }
	funcs["labelScope"] = func(wsID int64) string { return labelScopeFor(lang, wsID) }
}

// fmtTimeFor renders a timestamp in the locale's compact form.
func fmtTimeFor(lang string, t time.Time) string {
	if t.IsZero() {
		return ""
	}
	if lang == "ja" {
		return t.Local().Format("2006年1月2日 15:04")
	}
	return t.Local().Format("Jan 2, 2006 15:04")
}

// authorNameFor renders a post's author; a nil author is an anonymous post.
func authorNameFor(lang string, u *store.User) string {
	if u == nil {
		return messagesFor(lang).Anonymous
	}
	return u.Name
}

// hideReasonLabelFor renders a moderation hide-reason code (stored
// verbatim in the DB) as display text.
func hideReasonLabelFor(lang, code string) string {
	if lang == "ja" {
		switch code {
		case store.HideReasonSpam:
			return "スパム"
		case store.HideReasonAbuse:
			return "不適切"
		case store.HideReasonOffTopic:
			return "話題外"
		case store.HideReasonOutdated:
			return "古い情報"
		case store.HideReasonDuplicate:
			return "重複"
		case store.HideReasonResolved:
			return "解決済み"
		case store.HideReasonLowQuality:
			return "低品質"
		}
	}
	return code
}

// nounFor renders a post-kind key ("comment", "thread", "reply") as a
// lowercase noun for mid-sentence use.
func nounFor(lang, key string) string {
	if lang == "ja" {
		switch key {
		case "comment":
			return "コメント"
		case "thread":
			return "スレッド"
		case "reply":
			return "返信"
		}
		return key
	}
	return key
}

// nounTitleFor is nounFor for sentence-leading use ("Thread restored.").
func nounTitleFor(lang, key string) string {
	if lang == "ja" {
		return nounFor(lang, key)
	}
	switch key {
	case "comment":
		return "Comment"
	case "thread":
		return "Thread"
	case "reply":
		return "Reply"
	}
	return key
}

// repliesClauseFor renders the " and its N replies" tail of the comment
// delete warning; n == 0 means no tail at all.
func repliesClauseFor(lang string, n int) string {
	if n <= 0 {
		return ""
	}
	if lang == "ja" {
		return fmt.Sprintf(" とその %d 件の返信", n)
	}
	if n == 1 {
		return fmt.Sprintf(" and its %d reply", n)
	}
	return fmt.Sprintf(" and its %d replies", n)
}

// labelKindFor renders a label kind ("issue"/"discussion") for display.
func labelKindFor(lang, kind string) string {
	if lang == "ja" {
		switch kind {
		case store.KindLabelIssue:
			return "イシュー"
		case store.KindLabelDiscussion:
			return "ディスカッション"
		}
		return kind
	}
	switch kind {
	case store.KindLabelIssue:
		return "Issue"
	case store.KindLabelDiscussion:
		return "Discussion"
	}
	return kind
}

// labelScopeFor renders a label's scope from its workspace id (0 = shared).
func labelScopeFor(lang string, wsID int64) string {
	if wsID != 0 {
		if lang == "ja" {
			return "ワークスペース"
		}
		return "workspace"
	}
	if lang == "ja" {
		return "共有"
	}
	return "shared"
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
		if q := r.URL.Query().Get("lang"); validLang(q) && r.URL.Path != "/locale" {
			setLangCookie(w, q)
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

// setLangCookie writes the lang cookie shared by ?lang= and /locale.
func setLangCookie(w http.ResponseWriter, lang string) {
	http.SetCookie(w, &http.Cookie{
		Name:     langCookieName,
		Value:    lang,
		Path:     "/",
		MaxAge:   365 * 24 * 3600,
		SameSite: http.SameSiteLaxMode,
	})
}

// setLocale handles GET /locale?lang=<code>: the language-switcher link's
// target. Unlike the ?lang= middleware path, which cannot know the
// referer's query (a template never sees the current URL), this endpoint
// redirects back to the Referer so filters like ?q= survive the switch.
// The Referer is sanitized to a same-origin absolute path — anything else
// falls back to the root.
func setLocale(w http.ResponseWriter, r *http.Request) {
	lang := r.URL.Query().Get("lang")
	if !validLang(lang) {
		lang = supportedLangs[0]
	}
	setLangCookie(w, lang)
	target := "/"
	if ref, err := url.Parse(r.Referer()); err == nil && (ref.Host == "" || ref.Host == r.Host) {
		if t := ref.RequestURI(); strings.HasPrefix(t, "/") && !strings.HasPrefix(t, "//") {
			target = t
		}
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
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
// English. It is deliberately limited to the two supported locales; a real
// multi-locale deployment should use golang.org/x/text/language.
func wantsJapanese(header string) bool {
	type preference struct {
		q     float64
		order int
		set   bool
	}
	var ja, en preference
	order := 0
	for part := range strings.SplitSeq(header, ",") {
		tag, q, ok := parseAcceptLanguagePart(part)
		if !ok {
			continue
		}
		pref := preference{q: q, order: order, set: true}
		switch {
		case tag == "ja" || strings.HasPrefix(tag, "ja-"):
			if !ja.set || q > ja.q {
				ja = pref
			}
		case tag == "en" || strings.HasPrefix(tag, "en-"):
			if !en.set || q > en.q {
				en = pref
			}
		}
		order++
	}
	if !ja.set || ja.q == 0 {
		return false
	}
	if !en.set {
		return true
	}
	if ja.q != en.q {
		return ja.q > en.q
	}
	return ja.order < en.order
}

// parseAcceptLanguagePart parses one language-range plus its q parameter.
// Entries with malformed or out-of-range q values are ignored.
func parseAcceptLanguagePart(part string) (string, float64, bool) {
	pieces := strings.Split(part, ";")
	tag := strings.ToLower(strings.TrimSpace(pieces[0]))
	if tag == "" {
		return "", 0, false
	}
	q := 1.0
	for _, piece := range pieces[1:] {
		name, value, ok := strings.Cut(strings.TrimSpace(piece), "=")
		if !ok || !strings.EqualFold(name, "q") {
			continue
		}
		parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil || math.IsNaN(parsed) || parsed < 0 || parsed > 1 {
			return "", 0, false
		}
		q = parsed
	}
	return tag, q, true
}

// fieldErrText renders a store validation failure for the page's locale.
// A zero FieldError yields "". Unknown codes fall back to the store's
// English message (the JSON API keeps using it directly).
func fieldErrText(lang, field string, fe store.FieldError) string {
	if fe.Code == "" {
		return ""
	}
	m := messagesFor(lang)
	switch fe.Code {
	case store.ErrCodeRequired:
		switch field {
		case "title":
			return m.TitleRequired
		case "body":
			return m.BodyRequired
		case "category":
			return m.CategoryRequired
		case "name":
			return m.NameRequired
		case "color":
			return m.ColorRequired
		case "slug":
			return m.SlugRequired
		case "password":
			return m.PasswordRequired
		case "base":
			return m.ErrBaseRefRequired
		case "head":
			return m.ErrHeadRefRequired
		}
	case store.ErrCodeTooLong:
		return fmt.Sprintf(m.ErrTooLong, fieldLabelFor(lang, field), fe.Limit)
	case store.ErrCodeSlugPattern:
		return fmt.Sprintf(m.ErrSlugPattern, store.WorkspaceSlugPattern())
	case store.ErrCodeHexColor:
		return m.ErrColorHex
	}
	return fe.Message
}

// fieldLabelFor names an input field for mid-sentence use in messages
// like "X must be N characters or fewer."
func fieldLabelFor(lang, field string) string {
	if lang == "ja" {
		switch field {
		case "title":
			return "タイトル"
		case "body":
			return "本文"
		case "comment":
			return "コメント"
		case "name":
			return "名前"
		case "slug":
			return "スラッグ"
		case "description":
			return "説明"
		case "color":
			return "色"
		case "category":
			return "カテゴリ"
		case "password":
			return "パスワード"
		}
	}
	switch field {
	case "body":
		return "Body"
	case "comment":
		return "Comment"
	case "name":
		return "Name"
	case "slug":
		return "Slug"
	case "description":
		return "Description"
	case "color":
		return "Color"
	case "category":
		return "Category"
	case "password":
		return "Password"
	}
	return "Title"
}

var jsDictCache sync.Map

// jsDictFor marshals the JS-layer strings for lang into the JSON blob the
// <html data-i18n> attribute carries; /static/i18n.js parses it. The
// catalogs are immutable, so the JSON form is shared per locale instead of
// being marshaled for every render or Renderer construction.
func jsDictFor(lang string) template.HTMLAttr {
	if cached, ok := jsDictCache.Load(lang); ok {
		return cached.(template.HTMLAttr)
	}
	m := messagesFor(lang)
	b, _ := json.Marshal(map[string]string{
		"toast_notifications": m.JSToastNotifications,
		"toast_close":         m.JSToastClose,
		"posting":             m.JSPosting,
		"updated":             m.JSUpdated,
		"comment_posted":      m.JSCommentPosted,
		"comment_post_failed": m.JSCommentPostFailed,
		"copied":              m.JSCopied,
		"copy_failed":         m.JSCopyFailed,
		"back_to":             m.JSBackTo,
		"noun_deleted":        m.JSNounDeleted,
		"entity_updated":      m.JSEntityUpdated,
		"answer_updated":      m.JSAnswerUpdated,
		"new_thread":          m.JSNewThread,
		"new_comment":         m.JSNewComment,
		"new_reply":           m.JSNewReply,
		"noun_issue":          m.JSNounIssue,
		"noun_discussion":     m.JSNounDiscussion,
		"noun_pr":             m.JSNounPR,
		"list_issues":         m.JSListIssues,
		"list_discussions":    m.JSListDiscussions,
		"list_prs":            m.JSListPRs,
		"write":               m.JSWrite,
		"preview":             m.JSPreview,
		"preview_empty":       m.JSPreviewEmpty,
		"preview_failed":      m.JSPreviewFailed,
		"write_for":           m.JSWriteFor,
		"preview_for":         m.JSPreviewFor,
	})
	dict := template.HTMLAttr(b)
	cached, _ := jsDictCache.LoadOrStore(lang, dict)
	return cached.(template.HTMLAttr)
}
