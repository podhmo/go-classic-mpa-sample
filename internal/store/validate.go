// Shared input validation: the field limits are domain rules enforced at
// every service boundary — the HTML forms and the JSON API (/api/v1) call
// the same functions so their validation cannot drift apart
// (docs/sketch/ja/cli-and-web-api.md, 検証 section).
package store

import (
	"fmt"
	"unicode/utf8"
)

const (
	// MaxTitleRunes is the title length limit for issues and discussions.
	MaxTitleRunes = 200
	// MaxBodyRunes is the length limit for bodies and comments.
	MaxBodyRunes = 10000
)

// FieldErrors maps an input field name ("title", "body", "category") to a
// user-facing validation message. nil means the input is valid.
type FieldErrors map[string]string

// Any reports whether any field failed validation.
func (e FieldErrors) Any() bool { return len(e) > 0 }

// ValidateIssueInput validates an IssueInput value.
func ValidateIssueInput(in IssueInput) FieldErrors {
	var errs FieldErrors
	switch {
	case in.Title == "":
		errs = FieldErrors{"title": "Title is required."}
	case utf8.RuneCountInString(in.Title) > MaxTitleRunes:
		errs = FieldErrors{"title": fmt.Sprintf("Title must be %d characters or fewer.", MaxTitleRunes)}
	}
	if utf8.RuneCountInString(in.Body) > MaxBodyRunes {
		if errs == nil {
			errs = FieldErrors{}
		}
		errs["body"] = fmt.Sprintf("Body must be %d characters or fewer.", MaxBodyRunes)
	}
	return errs
}

// ValidateDiscussionInput validates a DiscussionInput value. A discussion
// must belong to exactly one category; the caller checks the category
// exists via the store.
func ValidateDiscussionInput(in DiscussionInput) FieldErrors {
	errs := ValidateIssueInput(IssueInput{Title: in.Title, Body: in.Body})
	if in.CategoryID == 0 {
		if errs == nil {
			errs = FieldErrors{}
		}
		errs["category"] = "Category is required."
	}
	return errs
}

// ValidateCommentBody validates a comment body; issue and discussion
// comments share the same rule. "" means valid.
func ValidateCommentBody(body string) string {
	switch {
	case body == "":
		return "Body is required."
	case utf8.RuneCountInString(body) > MaxBodyRunes:
		return fmt.Sprintf("Comment must be %d characters or fewer.", MaxBodyRunes)
	}
	return ""
}
