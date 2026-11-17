# go-classic-mpa-sample

A small issue tracker + discussions app built as a **classic multi-page
application (MPA)** in Go — server-rendered HTML, plain HTML forms for
every state change, and the POST → Redirect → GET (PRG) pattern
everywhere. Two themes drive it:

- **Trying every modern web feature on an old design.** The
  request/response model stays untouched, but the UI is a testbed for new
  platform features: cross-document view transitions, anchor positioning,
  Invoker Commands, Speculation Rules, the Custom Highlight API,
  `field-sizing`, `light-dark()` / `contrast-color()` — plus a little
  hand-written JavaScript where declarative features don't reach.
- **Experiencing legacy-style web app development.** No SPA framework, no
  ORM, no build step, no Node toolchain — just `net/http`,
  `html/template` and SQLite, grown like a real codebase (accounts,
  discussions, a JSON API, a gh-style CLI) instead of a toy.

## Tech stack

- Go 1.27 (`net/http`, `html/template`, `embed`, `context`, …)
- `http.ServeMux` with Go 1.22+ method+path patterns (`GET /issues/{id}`)
- `database/sql` + SQLite via `modernc.org/sqlite` (no CGO)
- `yuin/goldmark` for Markdown (GFM + GitHub-style `> [!NOTE]` alerts)
  rendering of issue/discussion bodies and comments
- `x/crypto/argon2` (argon2id, PHC string form) for account passwords
- Hand-written HTML/CSS and a few one-purpose ES modules under `/static/` —
  no frameworks, no ORM, no build step

## Run

```sh
go run ./cmd/zatudan
```

Then open <http://localhost:8080> — `GET /` redirects to `/issues`.
On first start the app creates the schema and seeds demo issues, labels,
discussion categories and comments (only when the tables are empty).

> The server binds to `127.0.0.1` by default. Accounts exist for author
> attribution, but there is **no authorization** — signed in or not,
> anyone can do anything. Only bind to a public interface inside a
> trusted network.

### `init` subcommand

```sh
go run ./cmd/zatudan -db zatudan.db init     # or: go run ./cmd/zatudan -init
```

`init` creates the database file if needed, applies the complete schema
(all tables and their indexes), runs `ANALYZE` so the query planner has
statistics, prints a report, and exits without starting the server. The
same schema init already happens automatically at every startup, so this
step is purely optional — an explicit provisioning step for deployment
scripts and the like. It does **not** seed demo data, and re-running it is
safe: the schema DDL is idempotent and `ANALYZE` refreshes the planner
statistics on a populated database.

Flags must come **before** the `init` argument — Go's `flag` package stops
parsing at the first positional argument, so write `go run ./cmd/zatudan -db x.db
init`, not `go run ./cmd/zatudan init -db x.db`. `-addr` has no effect in init mode.

## Accounts

`GET/POST /register`, `GET/POST /login`, `POST /logout`. Phase 1 is
authentication only: posts and comments carry the author's name when
signed in, anonymous use still works, and nothing is permission-gated.
Passwords are argon2id hashes in PHC string form (parameters embedded, so
re-tuning needs no migration); sessions are opaque tokens and only their
SHA-256 is stored, so reading the table can't steal a live session.

## CLI

`cmd/zatu` is a separate gh-style binary — distributing the client must
not require shipping the server, so it is its own command:

```sh
go run ./cmd/zatu issue list
go run ./cmd/zatu issue view 3
go run ./cmd/zatu comment create 3 --body "nice catch"
go run ./cmd/zatu discussion list
go run ./cmd/zatu auth token --user alice          # mint a per-user API token
go run ./cmd/zatu completion zsh                   # bash|zsh|fish
```

Two backends, selected automatically:

- **local** (default): opens the SQLite file directly via `-db`/`ZATUDAN_DB`
  — no server needed. `auth token …` is local-only.
- **remote**: `--server`/`--token` or `ZATUDAN_SERVER`/`ZATUDAN_TOKEN` talks
  to `/api/v1`. The raw escape hatch is remote-only —
  `zatu api /api/v1/issues -f q=bug` (gh-style `-f`/`-F`/`-X`; `-f`
  fields become query params on GET, a JSON body otherwise).

## Test

```sh
go test ./...                                   # unit + store + web + api + cli
go test -tags=e2e -v ./internal/e2etests/       # CLI end-to-end against a live server
```

The web and API layers are tested with `httptest` against in-memory
`Store` implementations; the CLI is tested against both backends.

## Storybook

`make storybook` serves every page at <http://localhost:6006> with fake
data and replayed production headers (CSP included) — UI work needs no
database. It runs on `podhmo/minigo`, fetched once by `make setup-minigo`
into `third_party/minigo`. `make storybook-watch` adds live reload.
See `docs/ja/for-ui-designer.md`.

## Configuration

| Flag    | Env var       | Default          | Description                                                            |
| ------- | ------------- | ---------------- | ---------------------------------------------------------------------- |
| `-addr` | `ZATUDAN_ADDR` | `127.0.0.1:8080` | Listen address                                                         |
| `-db`   | `ZATUDAN_DB`   | `zatudan.db`      | SQLite database file path                                              |
| `-init` | —             | `false`          | Initialize the database and exit (same as the `init` subcommand)       |
| `-api-token` | `ZATUDAN_API_TOKEN` | *(unset)*   | Shared Bearer token for `/api/v1`; the API also mounts when per-user `api_tokens` exist |

Example: `go run ./cmd/zatudan -addr :8080 -db /tmp/my-zatudan.db` or
`ZATUDAN_ADDR=:8080 go run ./cmd/zatudan`.

Use `-db :memory:` for a throwaway database. `go run ./cmd/zatudan -db PATH init` (or
`-init`) prepares the database without serving — see the `init` subcommand
above; flags must precede the `init` argument.

The CLI reads its own variables: `ZATUDAN_DB` for local mode,
`ZATUDAN_SERVER` + `ZATUDAN_TOKEN` for remote mode.

## Routes

| Method | Path                                  | Purpose                                          |
| ------ | ------------------------------------- | ------------------------------------------------ |
| GET    | `/`                                   | 303 redirect to `/issues`                        |
| GET/POST | `/register`                         | Create an account (session cookie on success)    |
| GET/POST | `/login`                            | Sign in                                          |
| POST   | `/logout`                             | Sign out → 303                                   |
| GET    | `/issues`                             | Issue list: open/closed/all tabs, `?q=` search, `?label=` filter, `?sort=newest\|oldest`, `?page=` |
| GET    | `/issues/new`                         | Create form                                      |
| POST   | `/issues`                             | Create issue → 303 to `/issues/{id}`             |
| GET    | `/issues/{id}`                        | Detail: body, labels, comments, comment form, actions |
| GET/POST | `/issues/{id}/edit`                 | Edit form → 303 to detail                        |
| POST   | `/issues/{id}/status`                 | Toggle open/closed (hidden `status` field) → 303 |
| GET/POST | `/issues/{id}/delete`               | GET confirm page (safe) / POST delete → 303 to `/issues` |
| POST   | `/issues/{id}/comments`               | Add comment → 303 to `/issues/{id}#comment-N`    |
| GET    | `/discussions`                        | Discussion list: `?category=<slug>`, `?answered=`, `?q=`, `?sort=`, `?page=` |
| GET/POST | `/discussions/new` + `/discussions` | Create form → create → 303 to detail             |
| GET    | `/discussions/{id}`                   | Detail: category, threads with their replies |
| GET/POST | `/discussions/{id}/edit`            | Edit form → 303 to detail                        |
| GET/POST | `/discussions/{id}/delete`          | GET confirm page / POST delete → 303 to list     |
| POST   | `/discussions/{id}/comments`          | New thread, or reply (`thread_id`) → 303 to `#thread-N`/`#comment-N` |
| POST   | `/discussions/{id}/comments/{cid}/answer` | Mark/unmark a thread as the Q&A answer (qa only) → 303 |
| GET    | `/static/*`                           | Embedded CSS/JS assets (explicitly registered)   |

Notes:

- Successful mutations always answer `303 See Other` (PRG). Comment,
  answer and status forms additionally carry `data-partial`, so with JS
  enabled `partial-submit.js` posts them via `fetch` and swaps the
  returned HTML fragment instead of reloading (`X-Partial` request
  header); without JS the same forms do the classic round-trip.
- Validation failures re-render the same form with `422`, preserving input
  and showing field errors wired via `aria-invalid`/`aria-describedby`.
- Missing issues return a styled 404 page; method mismatches return 405
  automatically via ServeMux patterns.
- CSRF protection uses the stdlib `http.NewCrossOriginProtection()`
  middleware (rejects cross-site non-GET requests via `Sec-Fetch-Site` /
  `Origin`), and POST bodies are capped at 1 MiB with `http.MaxBytesReader`.
- Issue/discussion bodies and comments are stored as raw text and rendered
  as Markdown (GFM via `goldmark`, including GitHub-style
  `> [!NOTE|TIP|IMPORTANT|WARNING|CAUTION]` alerts) at display time.
  Raw HTML in the source is omitted and unsafe link schemes
  (`javascript:`, `data:`, …) are neutralized by the renderer, so
  stored-XSS is not possible.

## JSON API (`/api/v1`)

A machine-facing JSON API (design: `docs/sketch/ja/cli-and-web-api.md`),
mounted only when a credential exists — `-api-token`/`ZATUDAN_API_TOKEN`
set **or** at least one per-user token minted via `zatu auth token`.
With neither, `/api/v1` is not mounted at all (fail-closed). Every
request then needs `Authorization: Bearer <token>`. Credential presence
is decided once at startup — if you mint the first token while the
server is running, `/api/v1` stays 404 until it is restarted.

Two credential kinds:

- the shared env token — constant-time compared; its writes stay anonymous
- per-user `api_tokens` rows — writes carry that user's `author_id`

| Method | Path                                          | Purpose                                             |
| ------ | --------------------------------------------- | --------------------------------------------------- |
| GET    | `/api/v1/issues`                              | List: `?status=&q=&label=<name>&sort=&page=&per_page=` (cap 100) |
| POST   | `/api/v1/issues`                              | Create `{title, body, label_ids\|labels}` → 201     |
| GET    | `/api/v1/issues/{id}`                         | Issue + labels + comments in one call               |
| PATCH  | `/api/v1/issues/{id}`                         | Partial update (only provided fields change)        |
| POST   | `/api/v1/issues/{id}/status`                  | `{status: "open"\|"closed"}`                      |
| DELETE | `/api/v1/issues/{id}`                         | Delete → 204                                        |
| POST   | `/api/v1/issues/{id}/comments`                | `{body}` → 201                                      |
| GET    | `/api/v1/labels`                              | All labels                                          |
| GET    | `/api/v1/discussions`                         | List: `?category=&answered=&q=&sort=&page=`         |
| POST   | `/api/v1/discussions`                         | `{title, body, category_id\|category}` → 201        |
| GET    | `/api/v1/discussions/{id}`                    | Discussion + threads (replies nested)               |
| PATCH  | `/api/v1/discussions/{id}`                    | Partial update                                      |
| DELETE | `/api/v1/discussions/{id}`                    | Delete → 204                                        |
| POST   | `/api/v1/discussions/{id}/threads`            | `{body}` → 201 (new thread)                         |
| POST   | `/api/v1/discussions/{id}/threads/{tid}/comments` | `{body}` → 201 (reply to thread)                |
| POST   | `/api/v1/discussions/{id}/threads/{tid}/answer` | `{answer: bool}` mark/unmark the answer (qa)      |
| GET    | `/api/v1/discussion-categories`               | All categories                                      |

Notes:

- Responses are `application/json`; errors use
  `{"error":{"message": ..., "fields": {...}}}` (`fields` only for
  per-field 422s).
- Timestamps are RFC 3339 strings. Status changes stay a `POST` action
  endpoint, mirroring the HTML app's action routes.

## Data storage

All data — issues, comments, labels, discussions, users, sessions,
`api_tokens` — lives in a single SQLite file, `zatudan.db` by default
(path from `-db`/`ZATUDAN_DB`), created in the working directory. `*.db`
files are git-ignored. Deleting the file resets the app; the next start
re-seeds demo data.

## Modern web features in use

Declarative HTML/CSS first; JavaScript only where declarative features
don't reach, always as external `/static/` files (inline scripts stay
CSP-blocked), and always progressive enhancement — the same forms do a
full PRG round-trip without it.

- **Cross-document view transitions** — `@view-transition` opt-in; a fade
  on plain navigation and directional back/forward slides keyed to the
  document's own VT types
- **Anchor positioning** — the moving tab underline is a named
  view-transition element driven by `position-anchor`, no JS
- **`::details-content`** — animated open/close for reply `<details>`
- **Invoker Commands** — `command`/`commandfor` opens and closes the
  `<dialog>` delete confirmation (browsers without support still get the
  GET confirmation page); a custom `--copy-permalink` command backs each
  comment's copy-link button
- **Speculation Rules** — `Speculation-Rules` header + external
  `application/speculationrules+json` ruleset prefetches likely next pages
- **Custom Highlight API** — `?q=` hits marked in list titles
  (`search-highlight.js`, the project's first JS)
- **`field-sizing: content`** — textareas grow with input, capped in `lh`
- **`content-visibility: auto`** — cheap rendering for long comment lists
- **Dark mode** — `color-scheme` + `light-dark()` design tokens;
  `contrast-color()`/Relative Color Syntax computes label-badge text
- **Partial updates** — `partial-submit.js` posts eligible forms via
  `fetch` and swaps returned fragments inside an element-scoped
  `startViewTransition()`; success lands as a `popover` toast plus an
  `aria-live` announcement
- **Form UX / a11y** — `:user-invalid` styling, skip link, landmarks,
  breadcrumbs with `aria-current`, per-link accessible names

## Architecture

```
cmd/zatudan/       server binary: flags/env, store.Open, SeedDemoData,
                   http.Server with timeouts, graceful shutdown
cmd/zatu/          gh-style CLI binary (issue/comment/label/discussion/auth/api/completion)
cmd/storybook/     UI preview server (separate module, runs on minigo)
internal/store/    persistence: sqlite schema, queries, ErrNotFound contract.
                   Exposes *store.Store; no HTTP knowledge.
internal/web/      HTML layer: consumer-side Store interface, handlers,
                   html/template pages, embedded static assets. No SQL.
internal/api/      /api/v1 JSON surface: wire DTOs, Bearer auth, method dispatch
internal/cli/      command grammar over a Backend interface (store or HTTP)
internal/client/   HTTP Backend implementation for remote-mode CLI
internal/auth/     argon2id password hashes + opaque session tokens
internal/e2etests/ CLI end-to-end tests (build tag `e2e`)
internal/web/templates/  layout.html + one file per page ({{define "content"}}) + fragments.html
internal/web/static/     app.css + one-purpose ES modules
tools/todo-gc/     TODO.md garbage collector
```

Key design choices:

- **PRG everywhere**: forms POST, handlers mutate, then 303-redirect so
  reloads never resubmit and history stays clean.
- **State lives in URLs**: list filters (`status`, `q`, `label`,
  `category`, `answered`, `sort`, `page`) are query parameters — every
  filtered view is linkable, and pagination/tab links preserve the active
  filters.
- **One contract, three consumers**: `internal/web`, `internal/api` and
  `internal/cli` each declare the narrow `Store`-shaped interface they
  need; `*store.Store` satisfies all of them, tests substitute fakes, and
  the CLI swaps in `internal/client`'s HTTP implementation for remote
  mode — commands never learn which backend they are talking to.
- **Per-page template sets**: each page file defines `{{define "content"}}`
  and is parsed together with `layout.html`, keeping pages independent;
  `fragments.html` shares comment/partial markup between full pages and
  `X-Partial` responses.

## Intentionally not implemented

- **Authorization** — authentication exists for author attribution;
  permissions do not. The bind stays on localhost by default.
- **SPA-style JavaScript** — JS is allowed when the storybook can verify
  its behavior (external files only), but declarative HTML/CSS stays
  preferred; no HTMX/Turbo/frameworks (see `docs/ja/decisions.md`
  「JavaScript 方針」).
- **Label admin UI** — labels are seeded; issue↔label assignment is
  supported.
- **Discussion extras** — reactions, polls, pinning, category admin,
  issue→discussion conversion are v3 candidates (see `docs/ja/roadmap.md`).
- **i18n** — UI strings are English.
- **Docker** — a single binary is the deploy story.
