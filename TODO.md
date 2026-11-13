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

- [x] A1. Implement `Open`/`Close`: modernc.org/sqlite driver ("sqlite"), schema init, `PRAGMA foreign_keys=ON`, busy_timeout; support `:memory:` safely (shared-cache DSN or MaxOpenConns(1))
- [x] A2. Schema: `issues(id AUTOINCREMENT, title NOT NULL, body, status CHECK open/closed, created_at, updated_at)`, `comments(id, issue_id FK ON DELETE CASCADE, body NOT NULL, created_at)`, `labels(id, name UNIQUE, color)`, `issue_labels(issue_id, label_id, PK pair, FKs cascade)`, indexes for list filtering (status, created_at)
- [x] A3. All methods above with parameterized queries only; `Scan` into types; label population via single grouped query (avoid N+1 if simple)
- [x] A4. `SeedDemoData`: labels (Bug/Feature/Documentation/Enhancement + colors), ~6+ issues with variety: open/closed, multiple labels, comments, one long body, staggered created_at; only when issues table empty
- [x] A5. `internal/store/store_test.go`: `:memory:` DB per test — CRUD, status toggle, comments, list filters (status/query/label/sort/pagination+total), ErrNotFound, seed idempotency
- [x] A6. `go test ./internal/store/` passes; `go vet` clean. Do NOT touch files outside `internal/store/`, do NOT modify `go.mod`/`go.sum`. Commit to branch `impl/store` in the worktree.

## Track B — web layer (worktree `impl/web`, owns `main.go`, `internal/web/**`, `README.md` ONLY)

- [x] B1. `internal/web`: define narrow `Store` interface (consumer-side) listing exactly the contract methods; `func New(st Store) http.Handler` building `http.ServeMux` with method+path patterns
- [x] B2. Routes: `GET /`→303 `/issues`; `GET /issues` (list: status toggle, `q` keyword, `label` filter, `sort` newest/oldest, `page` pagination, query params preserved in links, empty-state message); `GET /issues/new`; `POST /issues`; `GET /issues/{id}` (detail+comments+comment form); `GET /issues/{id}/edit`; `POST /issues/{id}/edit`; `POST /issues/{id}/status` (toggle); `GET /issues/{id}/delete` (confirm page); `POST /issues/{id}/delete`; `POST /issues/{id}/comments`; `GET /static/app.css`
- [x] B3. PRG: all successful POSTs → `303 SeeOther`. 404 for missing issue (`errors.Is(err, store.ErrNotFound)`), 405 handled by mux patterns
- [x] B4. Validation server-side: title required (trim, ≤200), body optional (reasonable cap), comment body required; on error re-render form preserving input + `aria-describedby` error messages
- [x] B5. CSRF: wrap mux with `http.NewCrossOriginProtection()` (Go 1.25+ stdlib); also `http.MaxBytesReader` (~1MB) on POST bodies
- [x] B6. Templates in `internal/web/templates/` via `embed.FS`: shared `layout.html` (header/nav/footer), `issues.html`, `issue_detail.html`, `issue_form.html` (new+edit), `issue_delete.html`, `error.html`; `html/template` auto-escaping; template funcs for date formatting + query building; plaintext bodies with `white-space: pre-wrap`
- [x] B7. `internal/web/static/app.css` via `embed.FS`: light theme, neutral palette, consistent buttons/forms/badges/links, label colors, open/closed visual status (not color-only), responsive (desktop+mobile), visible `:focus` styles, semantic HTML, `label`↔input association, no JS/emoji icons
- [x] B8. `main.go`: flags `-addr` (default `127.0.0.1:8080`), `-db` (default `issues.db`, also allow env override); `store.Open` + `SeedDemoData`; `http.Server` with sensible timeouts (ReadHeaderTimeout etc.); graceful shutdown on SIGINT
- [x] B9. `internal/web/web_test.go`: `httptest` + in-memory fake Store — list renders, 404 on missing issue, validation error re-renders form, POST→303 redirect location, HTML escaping of `<script>`, comment post, status toggle, delete flow. (Real-SQLite integration is verified post-merge, so tests MUST use the fake.)
- [x] B10. `README.md` rewrite: overview, tech stack, `go run .` startup, `go test ./...`, flags/env vars, routes, DB location, architecture, intentionally-not-implemented list (auth, markdown, JS)
- [x] B11. `go build ./...`, `go test ./internal/web/`, `go vet` clean. Do NOT touch `internal/store/**`, `go.mod`, `go.sum`. Commit to branch `impl/web` in the worktree.

## Integration (me, post-merge)

- [x] I1. Merge `impl/store` + `impl/web` into `main`; `go mod tidy`; fix conflicts/compile errors
- [x] I2. `go build ./...`, `go vet ./...`, `go test ./...` (34 tests pass, -race clean)
- [x] I3. Smoke test: `go run .` → curl `/issues`, create/comment/toggle/delete flows, confirm SQLite file persists
- [x] I4. Dedicated QA pass (fresh subagent) → findings recorded in `docs/ja/bugs.md`

## Bug-fix round (worktrees `fix/store`, `fix/web`; see `docs/ja/bugs.md` for severity/detail)

Store (`internal/store/**` only):
- [x] F1. Unknown/duplicate `label_id` → 500: `setIssueLabels` → `INSERT OR IGNORE ... SELECT id FROM labels` (silently skip, consistent with web dropping non-numeric ids)
- [x] F2. `SetIssueStatus` bumps `updated_at` only when status actually changes (CASE WHEN)
- [x] F3. `CreateComment` bumps parent issue `updated_at` in same tx
- [x] F4. Tests: unknown+dup label ids on create/update, no-op status keeps updated_at, comment bumps updated_at

Web (`internal/web/**`, `main.go` only):
- [x] F5. Out-of-range `page`: parse bound (≤1e6, kill int64 overflow) + clamp to `TotalPages` & re-query so out-of-range shows last page with pagination
- [x] F6. Catch-all `"/"` → styled 404; explicit `GET /static/app.css` (kills `/static/` dir listing)
- [x] F7. `updateIssue`: existence check before validation re-render (missing issue → 404 not 422)
- [x] F8. Detail-page label links → `?status=all&label=N`
- [x] F9. `issuesURL`: omit `status` when open (default)
- [x] F10. main.go: reject `-db ""`
- [x] F11. Security headers middleware: nosniff + frame DENY + referrer-policy
- [x] F12. Regression tests per bug (see docs/ja/bugs.md)

Post-fix integration (me):
- [x] P1. Merge `fix/store` + `fix/web`; `go mod tidy`; build+vet+test+race
- [x] P2. Re-run smoke test incl. the fixed paths (bogus label_id, page=99999, /nonexistent styled 404)
- [x] P3. Update bugs.md statuses → fixed; update decisions/pitfalls; final checklist §11; cleanup worktrees; final report

## Refactor: typed Renderer (worktree `refactor/renderer`, `internal/web/**` only)

- [x] R1. Introduce `Renderer` struct owning the parsed page templates; typed methods `renderIssueList` / `renderIssueDetail` / `renderIssueForm` / `renderIssueDelete` / `renderError` (+ `notFound` convenience) taking concrete page-data types instead of `data any`
- [x] R2. `handler` uses the Renderer for all render call sites; `New(st Store) http.Handler` signature unchanged; keep buffer-then-write behavior and error-page fallback
- [x] R3. Existing tests still pass untouched; `go vet`/`gofmt` clean

## Test round 1 — E2E through the real stack (worktree `test/e2e`, new file `internal/web/integration_test.go`)

E2E = `store.Open(temp file db)` + `web.New(realStore)` + `httptest.NewServer` — exercises real SQL, real mux, CSRF middleware, real template rendering. Distinct from the fakeStore unit tests.

- [x] T1. Issue lifecycle: POST create → appears in `GET /issues` → `GET /issues/{id}` shows title/body/labels → POST edit changes title → POST status closed → GET shows Closed + absent from default list → POST delete → 303 → GET 404
- [x] T2. Comments & validation: POST comment → renders on detail with timestamp; empty/whitespace comment → 422 page containing error text + preserved draft; comment on missing issue → 404

## Test round 2 — remaining E2E (same file, after round 1 merges)

- [x] T3. List interactions on seeded data: `?status=closed` shows only closed; `?q=` matches title AND body; `?label=` filters; `?sort=oldest` reverses order; pagination `?page=2` + `?page=999` clamps
- [x] T4. CSRF through the real middleware: cross-site `Sec-Fetch-Site` POST → 403 with no mutation; same-origin POST → 303
- [x] T5. Seed sanity: `SeedDemoData` → list renders seeded issues with label badges; closed seed issues appear only under `?status=closed|all`

## Backlog — operational tooling (user request)

- [ ] O1. `init` subcommand / `-init` flag: `go run . init` (or `go run . -init`) creates the database file and lays down the complete schema — all tables WITH their indexes (`issues`, `comments`, `labels`, `issue_labels`, `issues(status, created_at)`-style indexes, any extra indexes deemed useful for list/search/label queries).
      Decided shape (user): explicit init step rather than a loose SQL script.
      Design questions to resolve at implementation time:
      - Does normal startup (`go run .`) still auto-init an empty DB (keep zero-config) or require `init` first (explicit)? Prefer keeping auto-init for `go run .` simplicity — `init` then acts as an explicit idempotent "create/verify schema" step usable for ops/`ANALYZE`.
      - Factor schema DDL so `init` and `store.Open` share one source of truth (e.g. exported `store.EnsureSchema`/`store.Init` or a `cmd` path that opens + migrates + exits).
      - `init` should print what it did (created file, applied N tables/indexes) and exit 0; safe to run twice.
      - Consider `ANALYZE` after init/seed for the sqlite query planner.
