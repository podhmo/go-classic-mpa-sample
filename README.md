# go-classic-mpa-sample

A small issue tracker built as a **classic multi-page application (MPA)** in
Go — server-rendered HTML, plain HTML forms for every state change, and
**zero JavaScript**.

It demonstrates that a modern-looking, usable web UI can be built with only
the request/response model: page navigation, forms, redirects, and URLs that
carry state in query parameters. Everything follows the
POST → Redirect → GET (PRG) pattern.

## Tech stack

- Go 1.27 (`net/http`, `html/template`, `embed`, `context`, …)
- `http.ServeMux` with Go 1.22+ method+path patterns (`GET /issues/{id}`)
- `database/sql` + SQLite via `modernc.org/sqlite` (no CGO)
- Hand-written HTML/CSS — no frameworks, no ORM, no build step

## Run

```sh
go run .
```

Then open <http://localhost:8080> — `GET /` redirects to `/issues`.
On first start the app creates the schema and seeds demo issues, labels and
comments (only when the issues table is empty).

> The server binds to `127.0.0.1` by default because there is **no
> authentication**. Only bind to a public interface inside a trusted
> network.

## Test

```sh
go test ./...
```

The web layer is tested with `httptest` against an in-memory `Store`
implementation (`internal/web/web_test.go`).

## Configuration

| Flag    | Env var       | Default          | Description                |
| ------- | ------------- | ---------------- | -------------------------- |
| `-addr` | `ISSUES_ADDR` | `127.0.0.1:8080` | Listen address             |
| `-db`   | `ISSUES_DB`   | `issues.db`      | SQLite database file path  |

Example: `go run . -addr :8080 -db /tmp/my-issues.db` or
`ISSUES_ADDR=:8080 go run .`.

Use `-db :memory:` for a throwaway database.

## Routes

| Method | Path                       | Purpose                                          |
| ------ | -------------------------- | ------------------------------------------------ |
| GET    | `/`                        | 303 redirect to `/issues`                        |
| GET    | `/issues`                  | Issue list: open/closed/all tabs, `?q=` search, `?label=` filter, `?sort=newest\|oldest`, `?page=` |
| GET    | `/issues/new`              | Create form                                      |
| POST   | `/issues`                  | Create issue → 303 to `/issues/{id}`             |
| GET    | `/issues/{id}`             | Detail: body, labels, comments, comment form, actions |
| GET    | `/issues/{id}/edit`        | Edit form                                        |
| POST   | `/issues/{id}/edit`        | Update issue → 303 to detail                     |
| POST   | `/issues/{id}/status`      | Toggle open/closed (hidden `status` field) → 303 |
| GET    | `/issues/{id}/delete`      | Delete confirmation page (safe — never deletes)  |
| POST   | `/issues/{id}/delete`      | Delete issue → 303 to `/issues`                  |
| POST   | `/issues/{id}/comments`    | Add comment → 303 to `/issues/{id}#comments`     |
| GET    | `/static/app.css`          | Embedded stylesheet                              |

Notes:

- Successful mutations always answer `303 See Other` (PRG).
- Validation failures re-render the same form with `422`, preserving input
  and showing field errors wired via `aria-describedby`.
- Missing issues return a styled 404 page; method mismatches return 405
  automatically via ServeMux patterns.
- CSRF protection uses the stdlib `http.NewCrossOriginProtection()`
  middleware (rejects cross-site non-GET requests via `Sec-Fetch-Site` /
  `Origin`), and POST bodies are capped at 1 MiB with `http.MaxBytesReader`.
- Issue bodies and comments are stored and rendered as plaintext
  (`white-space: pre-wrap`); `html/template` escaping prevents XSS.

## Data storage

All data lives in a single SQLite file, `issues.db` by default (path from
`-db`/`ISSUES_DB`), created in the working directory. `*.db` files are
git-ignored. Deleting the file resets the app; the next start re-seeds demo
data.

## Architecture

```
main.go            entry point: flags/env, store.Open, SeedDemoData,
                   http.Server with timeouts, graceful shutdown (SIGINT/SIGTERM)
internal/store/    persistence: sqlite schema, queries, ErrNotFound contract.
                   Exposes *store.Store; no HTTP knowledge.
internal/web/      HTTP layer: consumer-side `Store` interface (satisfied by
                   *store.Store), handlers, html/template pages + shared
                   layout, embedded static assets. No SQL knowledge.
internal/web/templates/  layout.html + one file per page ({{define "content"}})
internal/web/static/     app.css
```

Key design choices:

- **PRG everywhere**: forms POST, handlers mutate, then 303-redirect so
  reloads never resubmit and history stays clean.
- **State lives in URLs**: list filters (`status`, `q`, `label`, `sort`,
  `page`) are query parameters — every filtered view is linkable, and
  pagination/tab links preserve the active filters.
- **Consumer-side interface**: `internal/web` declares the narrow `Store`
  interface it needs; `*store.Store` satisfies it, tests substitute a fake.
- **Per-page template sets**: each page file defines `{{define "content"}}`
  and is parsed together with `layout.html`, keeping pages independent.

## Intentionally not implemented

- **Authentication / authorization** — single local user; bind stays on
  localhost by default.
- **JavaScript** — none, anywhere. Toggles and deletes are ordinary forms.
- **Markdown / rich text** — bodies and comments are plaintext.
- **Label admin UI** — labels are seeded; issue↔label assignment is supported.
- **i18n** — UI strings are English.
