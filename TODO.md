# TODO — Go classic MPA Issue Tracker

Source spec: `docs/ja/prompts.md` (Go 1.27+, `net/http`, `html/template`, `database/sql`, SQLite via `modernc.org/sqlite`, no JavaScript, no web frameworks, no ORM).

Module path: `github.com/podhmo/go-classic-mpa-sample`

## Shared contract (`internal/store`, defined in skeleton commit)

Types: `Issue`, `Comment`, `Label`, `IssueInput`, `ListFilter`, `ErrNotFound`.

```go
func Open(path string) (*Store, error)                                    // opens DB + runs schema init (IF NOT EXISTS), PRAGMA foreign_keys=ON
func (s *Store) Close() error
func (s *Store) SeedDemoData(ctx) error                                   // seeds labels + demo issues/comments ONLY if issues table is empty; else no-op
func (s *Store) ListIssues(ctx, f ListFilter) ([]*Issue, int, error)      // issues WITH Labels + total count (pre-LIMIT) for pagination
func (s *Store) GetIssue(ctx, id int64) (*Issue, error)                   // WITH Labels; ErrNotFound
func (s *Store) CreateIssue(ctx, in IssueInput) (int64, error)
func (s *Store) UpdateIssue(ctx, id int64, in IssueInput) error           // updates title/body/labels, bumps updated_at; ErrNotFound
func (s *Store) SetIssueStatus(ctx, id int64, status string) error        // "open"|"closed"; ErrNotFound
func (s *Store) DeleteIssue(ctx, id int64) error                          // cascades comments + label links; ErrNotFound
func (s *Store) ListComments(ctx, issueID int64) ([]*Comment, error)      // chronological
func (s *Store) CreateComment(ctx, issueID int64, body string) (int64, error) // ErrNotFound if issue missing
func (s *Store) ListLabels(ctx) ([]*Label, error)                         // ordered by name
```

`ListFilter` semantics: `Status` "" → "open"; `Query` = case-insensitive substring on title OR body; `Sort` "newest" (default) / "oldest"; `Page` 1-based (clamp <1→1); `PerPage` <=0 → 20.

Schema notes: `issues.id INTEGER PRIMARY KEY AUTOINCREMENT` (issue number = id, never reused after delete). Tables: `issues`, `comments`, `labels`, `issue_labels`. Timestamps UTC. For `:memory:` DSNs use shared cache / single conn so `database/sql` pooling works in tests.

## Track A — data layer (worktree `impl/store`, owns `internal/store/**` ONLY)

- [ ] A1. Implement `Open`/`Close`: modernc.org/sqlite driver ("sqlite"), schema init, `PRAGMA foreign_keys=ON`, busy_timeout; support `:memory:` safely (shared-cache DSN or MaxOpenConns(1))
- [ ] A2. Schema: `issues(id AUTOINCREMENT, title NOT NULL, body, status CHECK open/closed, created_at, updated_at)`, `comments(id, issue_id FK ON DELETE CASCADE, body NOT NULL, created_at)`, `labels(id, name UNIQUE, color)`, `issue_labels(issue_id, label_id, PK pair, FKs cascade)`, indexes for list filtering (status, created_at)
- [ ] A3. All methods above with parameterized queries only; `Scan` into types; label population via single grouped query (avoid N+1 if simple)
- [ ] A4. `SeedDemoData`: labels (Bug/Feature/Documentation/Enhancement + colors), ~6+ issues with variety: open/closed, multiple labels, comments, one long body, staggered created_at; only when issues table empty
- [ ] A5. `internal/store/store_test.go`: `:memory:` DB per test — CRUD, status toggle, comments, list filters (status/query/label/sort/pagination+total), ErrNotFound, seed idempotency
- [ ] A6. `go test ./internal/store/` passes; `go vet` clean. Do NOT touch files outside `internal/store/`, do NOT modify `go.mod`/`go.sum`. Commit to branch `impl/store` in the worktree.

## Track B — web layer (worktree `impl/web`, owns `main.go`, `internal/web/**`, `README.md` ONLY)

- [ ] B1. `internal/web`: define narrow `Store` interface (consumer-side) listing exactly the contract methods; `func New(st Store) http.Handler` building `http.ServeMux` with method+path patterns
- [ ] B2. Routes: `GET /`→303 `/issues`; `GET /issues` (list: status toggle, `q` keyword, `label` filter, `sort` newest/oldest, `page` pagination, query params preserved in links, empty-state message); `GET /issues/new`; `POST /issues`; `GET /issues/{id}` (detail+comments+comment form); `GET /issues/{id}/edit`; `POST /issues/{id}/edit`; `POST /issues/{id}/status` (toggle); `GET /issues/{id}/delete` (confirm page); `POST /issues/{id}/delete`; `POST /issues/{id}/comments`; `GET /static/app.css`
- [ ] B3. PRG: all successful POSTs → `303 SeeOther`. 404 for missing issue (`errors.Is(err, store.ErrNotFound)`), 405 handled by mux patterns
- [ ] B4. Validation server-side: title required (trim, ≤200), body optional (reasonable cap), comment body required; on error re-render form preserving input + `aria-describedby` error messages
- [ ] B5. CSRF: wrap mux with `http.NewCrossOriginProtection()` (Go 1.25+ stdlib); also `http.MaxBytesReader` (~1MB) on POST bodies
- [ ] B6. Templates in `internal/web/templates/` via `embed.FS`: shared `layout.html` (header/nav/footer), `issues.html`, `issue_detail.html`, `issue_form.html` (new+edit), `issue_delete.html`, `error.html`; `html/template` auto-escaping; template funcs for date formatting + query building; plaintext bodies with `white-space: pre-wrap`
- [ ] B7. `internal/web/static/app.css` via `embed.FS`: light theme, neutral palette, consistent buttons/forms/badges/links, label colors, open/closed visual status (not color-only), responsive (desktop+mobile), visible `:focus` styles, semantic HTML, `label`↔input association, no JS/emoji icons
- [ ] B8. `main.go`: flags `-addr` (default `127.0.0.1:8080`), `-db` (default `issues.db`, also allow env override); `store.Open` + `SeedDemoData`; `http.Server` with sensible timeouts (ReadHeaderTimeout etc.); graceful shutdown on SIGINT
- [ ] B9. `internal/web/web_test.go`: `httptest` + in-memory fake Store — list renders, 404 on missing issue, validation error re-renders form, POST→303 redirect location, HTML escaping of `<script>`, comment post, status toggle, delete flow. (Real-SQLite integration is verified post-merge, so tests MUST use the fake.)
- [ ] B10. `README.md` rewrite: overview, tech stack, `go run .` startup, `go test ./...`, flags/env vars, routes, DB location, architecture, intentionally-not-implemented list (auth, markdown, JS)
- [ ] B11. `go build ./...`, `go test ./internal/web/`, `go vet` clean. Do NOT touch `internal/store/**`, `go.mod`, `go.sum`. Commit to branch `impl/web` in the worktree.

## Integration (me, post-merge)

- [ ] I1. Merge `impl/store` + `impl/web` into `main`; `go mod tidy`; fix conflicts/compile errors
- [ ] I2. `go build ./...`, `go vet ./...`, `go test ./...`
- [ ] I3. Smoke test: `go run .` → curl `/issues`, create/comment/toggle/delete flows, confirm SQLite file persists
- [ ] I4. Verify checklist §11 of prompts.md; final report
