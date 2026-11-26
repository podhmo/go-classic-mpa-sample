package store

import (
	"strings"
	"unicode"
)

// IssueSearchQuery is the parsed form of the issue list's shared search
// syntax. Recognized qualifiers are ANDed with the remaining text terms.
type IssueSearchQuery struct {
	Terms    []string
	Statuses []string
	Labels   []string
	Authors  []string
}

// ParseIssueSearchQuery parses `is:open label:bug author:demo foo`.
// Unknown or malformed qualifier tokens stay plain search terms. Quoting a
// value with single or double quotes allows spaces inside it.
func ParseIssueSearchQuery(q string) IssueSearchQuery {
	var out IssueSearchQuery
	for _, token := range searchTokens(q) {
		key, value, ok := strings.Cut(token, ":")
		if !ok || value == "" {
			out.Terms = append(out.Terms, token)
			continue
		}
		switch strings.ToLower(key) {
		case "is":
			if value == StatusOpen || value == StatusClosed {
				out.Statuses = appendUnique(out.Statuses, value)
			} else {
				out.Terms = append(out.Terms, token)
			}
		case "label":
			out.Labels = appendUnique(out.Labels, value)
		case "author":
			out.Authors = appendUnique(out.Authors, value)
		default:
			out.Terms = append(out.Terms, token)
		}
	}
	return out
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func searchTokens(s string) []string {
	var out []string
	var b strings.Builder
	var quote rune
	emptyQuoted := false
	flush := func() {
		if b.Len() > 0 || emptyQuoted {
			out = append(out, b.String())
			b.Reset()
			emptyQuoted = false
		}
	}
	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				b.WriteRune(r)
			}
		case (r == '\'' || r == '"') && (b.Len() == 0 || strings.HasSuffix(b.String(), ":")):
			quote = r
			if b.Len() == 0 {
				emptyQuoted = true
			}
		case unicode.IsSpace(r):
			flush()
		default:
			b.WriteRune(r)
		}
	}
	flush()
	return out
}
