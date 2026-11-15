// Output helpers: --json field selection, views shared by commands, and
// the -w/--web browser opener.
package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/podhmo/go-classic-mpa-sample/internal/api"
	"github.com/podhmo/go-classic-mpa-sample/internal/store"
)

// fmtTimeShort renders a timestamp for table output.
func fmtTimeShort(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Local().Format("2006-01-02 15:04")
}

// authorName renders a post's author; nil is an anonymous post.
func authorName(u *store.User) string {
	if u == nil {
		return "anonymous"
	}
	return u.Name
}

// --- store → api DTO views ---
//
// --json output uses the api package's wire field names so `issues issue
// list --json` and `issues api /issues` speak the same JSON vocabulary.

func issueViewJSON(i *store.Issue) api.Issue {
	out := api.Issue{
		ID: i.ID, Title: i.Title, Body: i.Body, Status: i.Status,
		Author:    userView(i.Author),
		CreatedAt: i.CreatedAt.Format(time.RFC3339), UpdatedAt: i.UpdatedAt.Format(time.RFC3339),
		Labels: []api.Label{},
	}
	for _, l := range i.Labels {
		out.Labels = append(out.Labels, api.Label{ID: l.ID, Name: l.Name, Color: l.Color})
	}
	return out
}

func issueViews(issues []*store.Issue) []api.Issue {
	out := make([]api.Issue, 0, len(issues))
	for _, i := range issues {
		out = append(out, issueViewJSON(i))
	}
	return out
}

func commentViews(cs []*store.Comment) []api.Comment {
	out := make([]api.Comment, 0, len(cs))
	for _, c := range cs {
		out = append(out, api.Comment{
			ID: c.ID, Body: c.Body, Author: userView(c.Author),
			CreatedAt: c.CreatedAt.Format(time.RFC3339),
		})
	}
	return out
}

func labelViews(labels []*store.Label) []api.Label {
	out := make([]api.Label, 0, len(labels))
	for _, l := range labels {
		out = append(out, api.Label{ID: l.ID, Name: l.Name, Color: l.Color})
	}
	return out
}

func userView(u *store.User) *api.User {
	if u == nil {
		return nil
	}
	return &api.User{ID: u.ID, Name: u.Name}
}

// writeJSONFields writes v as JSON restricted to the comma-separated top-
// level field names, like gh's `--json field1,field2`. For a slice input
// the selection applies to each element.
func writeJSONFields(w io.Writer, v any, fields string) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	var generic any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return err
	}
	want := map[string]bool{}
	for _, f := range strings.Split(fields, ",") {
		f = strings.TrimSpace(f)
		if f != "" {
			want[f] = true
		}
	}
	if len(want) == 0 {
		return usageError{msg: "--json requires at least one field name"}
	}
	pick := func(m map[string]any) map[string]any {
		out := map[string]any{}
		for k, val := range m {
			if want[k] {
				out[k] = val
			}
		}
		return out
	}
	var out any
	switch g := generic.(type) {
	case []any:
		list := make([]any, 0, len(g))
		for _, e := range g {
			if m, ok := e.(map[string]any); ok {
				list = append(list, pick(m))
			} else {
				list = append(list, e)
			}
		}
		out = list
	case map[string]any:
		out = pick(g)
	default:
		out = generic
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func closeQuietly(c io.Closer) {
	if c != nil {
		c.Close()
	}
}

// openBrowser asks the OS to open url. Linux uses xdg-open, macOS `open`,
// Windows rundll32 — the same trio every stdlib-only CLI ships.
func openBrowser(url string) error {
	var name string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		name, args = "open", []string{url}
	case "windows":
		name, args = "rundll32", []string{"url.dll,FileProtocolHandler", url}
	default:
		name, args = "xdg-open", []string{url}
	}
	if err := exec.Command(name, args...).Start(); err != nil {
		return fmt.Errorf("open browser (%s): %w", name, err)
	}
	return nil
}
