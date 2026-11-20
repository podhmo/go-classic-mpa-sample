# go-classic-mpa-sample

A small issue tracker + discussions app with **git-backed pull requests**,
built as a **classic multi-page application (MPA)** in Go — server-rendered
HTML, plain HTML forms for every state change, and the POST → Redirect →
GET (PRG) pattern everywhere. Two themes drive it:

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
On first start the app creates the schema and its master data (the default
workspace, labels, discussion categories). Demo issues, discussions,
comments and the `demo`/`demo` account are mock data and only land when
`-seed` (env `ZATUDAN_SEED`) is set — without it the app starts empty.
Pull requests have no seed flag: `tools/demo-pr-history.sh [DB] [REPO]`
builds a small fixture repository and replays a create/comment/close/merge
history through `zatu pr`.

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
scripts and the like. It does **not** seed demo data unless `-seed` is
passed, and re-running it is safe: the schema DDL is idempotent and
`ANALYZE` refreshes the planner statistics on a populated database.

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
go run ./cmd/zatu issue view 3 --markdown               # the issue + comments as one markdown doc
go run ./cmd/zatu comment create 3 --body "nice catch"
go run ./cmd/zatu discussion list
go run ./cmd/zatu pr list                          # pull requests
go run ./cmd/zatu pr view 1 --markdown             # the PR + commits + review comments as one doc
go run ./cmd/zatu pr create --base main --title "…" # head defaults to the current branch
go run ./cmd/zatu pr merge 1 --method squash
go run ./cmd/zatu auth token --user alice          # mint a per-user API token
go run ./cmd/zatu completion zsh                   # bash|zsh|fish
```

Two backends, selected automatically:

- **local** (default): opens the SQLite file directly via `-db`/`ZATUDAN_DB`
  — no server needed. `auth token …` is local-only. The `pr` verbs
  additionally need a git repository: pass `--repo`/`ZATUDAN_REPO` or let
  it auto-detect the `.git` enclosing `-db` (no repo → `not a git
  repository`, exit 1).
- **remote**: `--server`/`--token` or `ZATUDAN_SERVER`/`ZATUDAN_TOKEN` talks
  to `/api/v1`. The raw escape hatch is remote-only —
  `zatu api /api/v1/issues -f q=bug` (gh-style `-f`/`-F`/`-X`; `-f`
  fields become query params on GET, a JSON body otherwise). `--repo`
  is meaningless here and warns it is ignored.

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
| `-seed` | `ZATUDAN_SEED` | `false`         | Insert demo issues, discussions and the demo user; master data is ensured either way |
| `-api-token` | `ZATUDAN_API_TOKEN` | *(unset)*   | Shared Bearer token for `/api/v1`; the API also mounts when per-user `api_tokens` exist |
| `-repo` | `ZATUDAN_REPO` | auto-detect near `-db` | Git repository enabling pull requests; `off` disables the feature |

Example: `go run ./cmd/zatudan -addr :8080 -db /tmp/my-zatudan.db` or
`ZATUDAN_ADDR=:8080 go run ./cmd/zatudan`.

Use `-db :memory:` for a throwaway database. `go run ./cmd/zatudan -db PATH init` (or
`-init`) prepares the database without serving — see the `init` subcommand
above; flags must precede the `init` argument.

The CLI reads its own variables: `ZATUDAN_DB` for local mode,
`ZATUDAN_SERVER` + `ZATUDAN_TOKEN` for remote mode, `ZATUDAN_REPO` for the
`zatu pr` git backend.

## Routes

The full HTML route table (issues, discussions, `/pulls`, moderation,
fragments) lives in [docs/ui.md](docs/ui.md#routes). All state changes
use plain POST forms followed by a 303 (PRG).
## JSON API (`/api/v1`)

A machine-facing JSON API mirrors the web app (issues, discussions,
labels, workspaces, pull requests) plus the `zatu` CLI's remote mode.
Mounted only when a credential exists — `-api-token`/
`ZATUDAN_API_TOKEN` or a per-user token minted via `zatu auth token`;
otherwise `/api/v1` is not mounted at all. The endpoint table and notes
live in [docs/api.md](docs/api.md).
## Data storage

All data — issues, comments, labels, discussions, users, sessions,
`api_tokens`, pull requests and review comments — lives in a single
SQLite file, `zatudan.db` by default
(path from `-db`/`ZATUDAN_DB`), created in the working directory. `*.db`
files are git-ignored. Deleting the file resets the app; the next start
re-seeds demo data.

Pull requests are DB rows carrying `(base_ref, head_ref)` plus a real git
repository (`-repo`/`ZATUDAN_REPO`, auto-detected by walking up from
`-db`; `off` disables the feature and every `/pulls` route). While a PR
is open its commits, diff and mergeability are computed live from git;
closing or merging pins the head tip under `refs/zatudan/pr/<id>/head`
so the recorded view survives branch deletion.

## Modern web features in use

Declarative HTML/CSS first; JavaScript only where declarative features
don't reach, always as external `/static/` files (inline scripts stay
CSP-blocked), and always progressive enhancement. The catalog — view
transitions, anchor positioning, Invoker Commands, Speculation Rules,
SSE live updates, partial submits and more — lives in
[docs/ui.md](docs/ui.md#modern-web-features-in-use).
## Architecture

```
cmd/zatudan/       server binary: flags/env, store.Open, SeedMasterData
                   (+ SeedDemoData under -seed), http.Server with
                   timeouts, graceful shutdown
cmd/zatu/          gh-style CLI binary (issue/comment/label/discussion/pr/auth/api/completion)
cmd/storybook/     UI preview server (separate module, runs on minigo)
internal/store/    persistence: sqlite schema, queries, ErrNotFound contract.
                   Exposes *store.Store; no HTTP knowledge.
internal/web/      HTML layer: consumer-side Store interface, handlers,
                   html/template pages, embedded static assets. No SQL.
internal/api/      /api/v1 JSON surface: wire DTOs, Bearer auth, method dispatch
internal/cli/      command grammar over a Backend interface (store or HTTP)
internal/client/   HTTP Backend implementation for remote-mode CLI
internal/export/   one markdown document per issue/discussion/pull request — shared by
                   the API, the web /markdown routes and `zatu view --markdown`
internal/gitrepo/  git operations for pull requests: repo resolution, log/diff/
                   merge-base wrappers, merge execution (worktree + plumbing paths)
internal/auth/     argon2id password hashes + opaque session tokens
internal/e2etests/ CLI end-to-end tests (build tag `e2e`)
internal/web/templates/  layout.html + one file per page ({{define "content"}}) + fragments.html
internal/web/static/     app.css + one-purpose ES modules
tools/todo-gc/     TODO.md garbage collector
tools/demo-history.sh / demo-pr-history.sh — replay demo content via `zatu` / `zatu pr`
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
- **i18n** — UI strings are English and Japanese (`?lang=` or the nav
  switcher sets a cookie; `Accept-Language` is the fallback). User
  content stays untranslated.
- **Docker** — a single binary is the deploy story.
