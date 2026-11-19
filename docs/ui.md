# UI

The server-rendered HTML surface: routes, conventions and the modern
web features in use. See [../README.md](../README.md) for the overview,
[api.md](api.md) for the JSON API, and
[ja/ui-rules.md](ja/ui-rules.md) for the consolidated UI rules.

## Routes

| Method | Path                                  | Purpose                                          |
| ------ | ------------------------------------- | ------------------------------------------------ |
| GET    | `/`                                   | 303 redirect to `/issues`                        |
| GET/POST | `/workspaces` + `/workspaces/new`   | Workspace list / create (routes below live under `/w/{ws}`) |
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
| GET    | `/issues/{id}/markdown`               | The issue + comments as one `text/markdown` document |
| GET    | `/discussions`                        | Discussion list: `?category=<slug>`, `?answered=`, `?q=`, `?sort=`, `?page=` |
| GET/POST | `/discussions/new` + `/discussions` | Create form → create → 303 to detail             |
| GET    | `/discussions/{id}`                   | Detail: category, threads with their replies |
| GET/POST | `/discussions/{id}/edit`            | Edit form → 303 to detail                        |
| GET/POST | `/discussions/{id}/delete`          | GET confirm page / POST delete → 303 to list     |
| POST   | `/discussions/{id}/comments`          | New thread, or reply (`thread_id`) → 303 to `#thread-N`/`#comment-N` |
| GET    | `/discussions/{id}/markdown`          | The discussion + threads + replies as `text/markdown` |
| POST   | `/discussions/{id}/comments/{cid}/answer` | Mark/unmark a thread as the Q&A answer (qa only) → 303 |
| GET    | `/pulls`                              | PR list: `?status=open\|closed\|merged\|all`, `?q=`, `?sort=`, `?page=` (repo-gated) |
| GET/POST | `/pulls/new` + `/pulls`             | Create form → create → 303 to `/pulls/{id}`      |
| GET    | `/pulls/{id}`                         | Detail: status, mergeability, commits, comments, comment form |
| GET/POST | `/pulls/{id}/edit`                  | Edit title/body → 303 to detail                  |
| GET    | `/pulls/{id}/files`                   | Merge-base→head diff, per-line anchored comment forms |
| POST   | `/pulls/{id}/comments`                | Comment (`path`+`line`+`side` anchors to a diff line) → 303 |
| GET/POST | `/pulls/{id}/comments/{cid}/edit` + `…/delete` | Comment edit / GET confirm + POST delete |
| POST   | `/pulls/{id}/comments/{cid}/hide` + `…/unhide` | Reasoned minimize/restore           |
| POST   | `/pulls/{id}/status`                  | Close / reopen → 303                             |
| GET/POST | `/pulls/{id}/merge`                 | Confirm page (method radios) → merge → 303       |
| GET    | `/static/*`                           | Embedded CSS/JS assets (explicitly registered)   |

Notes:

- Routes are workspace-scoped: the canonical form of every path in the
  table carries a `/w/{ws}` prefix (e.g. `/w/main/issues/3`), and the
  workspace-less forms above redirect into the right workspace
  (302 for GET/HEAD, 307 otherwise) — **except `/pulls*`**, which is
  workspace-independent (PRs belong to the repo, not a workspace) and is
  only mounted when a git repo is resolved; without one the nav link
  hides and every `/pulls*` route falls through to 404. The `/markdown`
  rows are the cookie-session counterpart of the API endpoints below —
  the detail pages' "Copy as markdown" buttons fetch them.
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
  comment's copy-link button and a `--copy-markdown` command backs the
  detail pages' copy-as-markdown button
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
- **Server-sent live updates** — `live-updates.js` opens an `EventSource`
  on detail pages (`{kind}:{id}/events`); when another tab/user mutates
  the record the hub pushes a topic and the page swaps the affected
  fragment
- **Form UX / a11y** — `:user-invalid` styling, skip link, landmarks,
  breadcrumbs with `aria-current`, per-link accessible names
