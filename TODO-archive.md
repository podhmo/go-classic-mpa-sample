# TODO archive

> **What this file is:** append-only history of completed work moved out of
> `TODO.md` by a periodic GC sweep (see `.agents/skills/todo-md/SKILL.md` —
> feature PRs never write here, so this file never conflicts). Each item
> lands at the end of the file as one line, prefixed with its ancestor
> chain (`- [x] A > B > **C**: ...`), so a grep hit names the whole path.
> Wording and links are kept verbatim; only the layout is normalized.
>
> **Reading this file:** do not read it whole — `grep` for a symbol or
> feature name; the matching line is the whole entry.

- [x] A1. Implement `Open`/`Close`: modernc.org/sqlite driver ("sqlite"), schema init, `PRAGMA foreign_keys=ON`, busy_timeout; support `:memory:` safely (shared-cache DSN or MaxOpenConns(1))
- [x] A2. Schema: `issues(id AUTOINCREMENT, title NOT NULL, body, status CHECK open/closed, created_at, updated_at)`, `comments(id, issue_id FK ON DELETE CASCADE, body NOT NULL, created_at)`, `labels(id, name UNIQUE, color)`, `issue_labels(issue_id, label_id, PK pair, FKs cascade)`, indexes for list filtering (status, created_at)
- [x] A3. All methods above with parameterized queries only; `Scan` into types; label population via single grouped query (avoid N+1 if simple)
- [x] A4. `SeedDemoData`: labels (Bug/Feature/Documentation/Enhancement + colors), ~6+ issues with variety: open/closed, multiple labels, comments, one long body, staggered created_at; only when issues table empty
- [x] A5. `internal/store/store_test.go`: `:memory:` DB per test — CRUD, status toggle, comments, list filters (status/query/label/sort/pagination+total), ErrNotFound, seed idempotency
- [x] A6. `go test ./internal/store/` passes; `go vet` clean. Do NOT touch files outside `internal/store/`, do NOT modify `go.mod`/`go.sum`. Commit to branch `impl/store` in the worktree.
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
- [x] I1. Merge `impl/store` + `impl/web` into `main`; `go mod tidy`; fix conflicts/compile errors
- [x] I2. `go build ./...`, `go vet ./...`, `go test ./...` (34 tests pass, -race clean)
- [x] I3. Smoke test: `go run .` → curl `/issues`, create/comment/toggle/delete flows, confirm SQLite file persists
- [x] I4. Dedicated QA pass (fresh subagent) → findings recorded in `docs/ja/bugs.md`
- [x] F1. Unknown/duplicate `label_id` → 500: `setIssueLabels` → `INSERT OR IGNORE ... SELECT id FROM labels` (silently skip, consistent with web dropping non-numeric ids)
- [x] F2. `SetIssueStatus` bumps `updated_at` only when status actually changes (CASE WHEN)
- [x] F3. `CreateComment` bumps parent issue `updated_at` in same tx
- [x] F4. Tests: unknown+dup label ids on create/update, no-op status keeps updated_at, comment bumps updated_at
- [x] F5. Out-of-range `page`: parse bound (≤1e6, kill int64 overflow) + clamp to `TotalPages` & re-query so out-of-range shows last page with pagination
- [x] F6. Catch-all `"/"` → styled 404; explicit `GET /static/app.css` (kills `/static/` dir listing)
- [x] F7. `updateIssue`: existence check before validation re-render (missing issue → 404 not 422)
- [x] F8. Detail-page label links → `?status=all&label=N`
- [x] F9. `issuesURL`: omit `status` when open (default)
- [x] F10. main.go: reject `-db ""`
- [x] F11. Security headers middleware: nosniff + frame DENY + referrer-policy
- [x] F12. Regression tests per bug (see docs/ja/bugs.md)
- [x] P1. Merge `fix/store` + `fix/web`; `go mod tidy`; build+vet+test+race
- [x] P2. Re-run smoke test incl. the fixed paths (bogus label_id, page=99999, /nonexistent styled 404)
- [x] P3. Update bugs.md statuses → fixed; update decisions/pitfalls; final checklist §11; cleanup worktrees; final report
- [x] R1. Introduce `Renderer` struct owning the parsed page templates; typed methods `renderIssueList` / `renderIssueDetail` / `renderIssueForm` / `renderIssueDelete` / `renderError` (+ `notFound` convenience) taking concrete page-data types instead of `data any`
- [x] R2. `handler` uses the Renderer for all render call sites; `New(st Store) http.Handler` signature unchanged; keep buffer-then-write behavior and error-page fallback
- [x] R3. Existing tests still pass untouched; `go vet`/`gofmt` clean
- [x] T1. Issue lifecycle: POST create → appears in `GET /issues` → `GET /issues/{id}` shows title/body/labels → POST edit changes title → POST status closed → GET shows Closed + absent from default list → POST delete → 303 → GET 404
- [x] T2. Comments & validation: POST comment → renders on detail with timestamp; empty/whitespace comment → 422 page containing error text + preserved draft; comment on missing issue → 404
- [x] T3. List interactions on seeded data: `?status=closed` shows only closed; `?q=` matches title AND body; `?label=` filters; `?sort=oldest` reverses order; pagination `?page=2` + `?page=999` clamps
- [x] T4. CSRF through the real middleware: cross-site `Sec-Fetch-Site` POST → 403 with no mutation; same-origin POST → 303
- [x] T5. Seed sanity: `SeedDemoData` → list renders seeded issues with label badges; closed seed issues appear only under `?status=closed|all`
- [x] M1. `github.com/yuin/goldmark/v2` v2.1.15 dependency (GFM extension, `html.WithHardWraps()`); raw HTML omitted, dangerous URL schemes neutralized by default renderer. Started on v1.8.7, then migrated to v2 for the official alert extension
- [x] M2. `internal/web/markdown.go`: `renderMarkdown` → `template.HTML` via `parser.New` + `html.New`; template func `markdown` in funcMap; detail template renders issue body + comments via it
- [x] M3. CSS: `.markdown-body` content styles (pre/code/blockquote/headings/lists/tables/hr/img); drop `white-space: pre-wrap` from `.issue-body`/`.comment-body`
- [x] M4. Tests: `markdown_test.go` unit tests + handler-level markdown test; fix E2E comment-body assertion for `<p>` wrapping
- [x] M5. Docs: decisions.md (new section), README (notes + tech stack), pitfalls.md; verified live via curl + playwright screenshot
- [x] M6. GitHub alerts `> [!NOTE|TIP|IMPORTANT|WARNING|CAUTION]` via `github.com/yuin/goldmark-alert` (official, v2-only); `.markdown-alert-*` CSS with per-kind colors + currentColor icons; unit test added
- [x] O1. `init` subcommand / `-init` flag: `go run . init` (or `go run . -init`) creates the database file and lays down the complete schema — all tables WITH their indexes (`issues`, `comments`, `labels`, `issue_labels`, `issues(status, created_at)`-style indexes, any extra indexes deemed useful for list/search/label queries). Decided shape (user): explicit init step rather than a loose SQL script. Design questions to resolve at implementation time:
- O1. `init` subcommand / `-init` flag > Does normal startup (`go run .`) still auto-init an empty DB (keep zero-config) or require `init` first (explicit)? Prefer keeping auto-init for `go run .` simplicity — `init` then acts as an explicit idempotent "create/verify schema" step usable for ops/`ANALYZE`.
- O1. `init` subcommand / `-init` flag > Factor schema DDL so `init` and `store.Open` share one source of truth (e.g. exported `store.EnsureSchema`/`store.Init` or a `cmd` path that opens + migrates + exits).
- O1. `init` subcommand / `-init` flag > `init` should print what it did (created file, applied N tables/indexes) and exit 0; safe to run twice.
- O1. `init` subcommand / `-init` flag > Consider `ANALYZE` after init/seed for the sqlite query planner.
