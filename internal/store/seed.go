package store

import (
	"context"
	"time"

	"github.com/podhmo/go-classic-mpa-sample/internal/auth"
)

type seedComment struct {
	body string
	age  time.Duration
}

type seedIssue struct {
	title    string
	body     string
	status   string
	age      time.Duration
	labels   []string
	comments []seedComment
}

var seedLabels = []Label{
	{Name: "Bug", Color: "#d73a4a"},
	{Name: "Documentation", Color: "#1f6feb"},
	{Name: "Enhancement", Color: "#a2eeef"},
	{Name: "Feature", Color: "#0075ca"},
}

var seedIssues = []seedIssue{
	{
		title:  "Crash when opening an empty project",
		body:   "The app panics with an index-out-of-range error when a project with no files is opened.\n\nSteps to reproduce:\n1. Create a new project\n2. Do not add any files\n3. Press Open",
		status: StatusOpen,
		age:    6*24*time.Hour + 12*time.Hour,
		labels: []string{"Bug"},
		comments: []seedComment{
			{body: "Reproduced on macOS and Linux. Windows seems unaffected.", age: 6*24*time.Hour + 6*time.Hour},
			{body: "Bisected to the changes in the file scanner last week.", age: 6 * 24 * time.Hour},
		},
	},
	{
		title:  "Add dark mode to the settings page",
		body:   "Several users asked for a dark theme. A `prefers-color-scheme` media query should cover most of it without any JavaScript.",
		status: StatusOpen,
		age:    5 * 24 * time.Hour,
		labels: []string{"Feature", "Enhancement"},
		comments: []seedComment{
			{body: "I can pick this up. Proposal: two CSS custom property sets toggled by the media query.", age: 4*24*time.Hour + 18*time.Hour},
			{body: "Sounds good. Please keep the contrast ratio at AA or better.", age: 4*24*time.Hour + 9*time.Hour},
			{body: "Draft palette posted; the dark surface color still needs work.", age: 4 * 24 * time.Hour},
		},
	},
	{
		title:  "Typo in the getting-started guide",
		body:   "\"Recieve\" should be \"Receive\" in the second paragraph.",
		status: StatusClosed,
		age:    4*24*time.Hour + 6*time.Hour,
		labels: []string{"Documentation"},
		comments: []seedComment{
			{body: "Fixed, thanks for the report.", age: 4 * 24 * time.Hour},
		},
	},
	{
		title: "Search slows down with 10,000+ records",
		body: `Search latency grows roughly linearly once the record count crosses about ten thousand. On a mid-range laptop a query that used to return instantly now takes around two seconds.

A quick profile shows almost all of the time is spent in the substring scan rather than in sorting or pagination, so an index alone probably will not fix it.

Possible directions: an FTS5 virtual table, a trigram index, or simply capping the scanned window when a filter already narrows the result set. Measurements for each option are welcome before we pick one.`,
		status: StatusOpen,
		age:    3 * 24 * time.Hour,
		labels: []string{"Bug", "Enhancement"},
		comments: []seedComment{
			{body: "Confirmed. At 50k records the same query takes ~9s.", age: 2*24*time.Hour + 20*time.Hour},
			{body: "FTS5 sounds promising; I will benchmark it against a trigram approach.", age: 2*24*time.Hour + 8*time.Hour},
		},
	},
	{
		title:  "Improve onboarding for first-time users",
		body:   "New users currently land on an empty list with no guidance. A short welcome tour or a set of sample items would help.\n\nIdeas: seed demo issues, add an inline hint on empty states, and link the guide from the header.",
		status: StatusOpen,
		age:    2 * 24 * time.Hour,
		labels: []string{"Feature", "Enhancement", "Documentation"},
		comments: []seedComment{
			{body: "Seeding a few demo issues on first launch seems like the cheapest win.", age: 24 * time.Hour},
		},
	},
	{
		title:  "Export issues as CSV",
		body:   "Needed for monthly reporting. Columns: id, title, status, labels, created_at, updated_at.",
		status: StatusOpen,
		age:    24*time.Hour + 3*time.Hour,
		labels: []string{"Feature"},
	},
	{
		title:  "Update screenshots in the user manual",
		body:   "The manual still shows the old two-column layout. Retake all screenshots with the current UI.",
		status: StatusClosed,
		age:    8 * time.Hour,
		labels: []string{"Documentation"},
		comments: []seedComment{
			{body: "Half of them are done; the list page ones remain.", age: 4 * time.Hour},
			{body: "All screenshots updated and merged.", age: 2 * time.Hour},
		},
	},
}

// SeedDemoData inserts the default labels plus demo issues/comments, but only
// when the issues table is empty. It is a no-op otherwise.
func (s *Store) SeedDemoData(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// The demo account lets people try the login flow out of the box. It is
	// created independently of the issue seeding below so a database that
	// already has issues (e.g. one upgraded to v2) still gets one. The
	// argon2id hash is computed at runtime; no credential literal is stored
	// in the source.
	demoHash, err := auth.HashPassword("demo")
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO users(name, password_hash, created_at)
			SELECT 'demo', ?, ?
			WHERE NOT EXISTS (SELECT 1 FROM users WHERE name = 'demo')`,
		demoHash, time.Now().UTC().Unix()); err != nil {
		return err
	}

	var n int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM issues").Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}

	labelIDs := make(map[string]int64, len(seedLabels))
	for _, l := range seedLabels {
		res, err := tx.ExecContext(ctx, "INSERT INTO labels(name, color) VALUES(?, ?)", l.Name, l.Color)
		if err != nil {
			return err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		labelIDs[l.Name] = id
	}

	now := time.Now().UTC()
	for _, si := range seedIssues {
		created := now.Add(-si.age).Unix()
		updated := created
		if si.status == StatusClosed {
			updated = now.Add(-si.age + 4*time.Hour).Unix()
		}
		res, err := tx.ExecContext(ctx,
			"INSERT INTO issues(title, body, status, created_at, updated_at) VALUES(?, ?, ?, ?, ?)",
			si.title, si.body, si.status, created, updated)
		if err != nil {
			return err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		for _, name := range si.labels {
			if _, err := tx.ExecContext(ctx,
				"INSERT INTO issue_labels(issue_id, label_id) VALUES(?, ?)", id, labelIDs[name]); err != nil {
				return err
			}
		}
		for _, c := range si.comments {
			if _, err := tx.ExecContext(ctx,
				"INSERT INTO comments(issue_id, body, created_at) VALUES(?, ?, ?)",
				id, c.body, now.Add(-c.age).Unix()); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}
