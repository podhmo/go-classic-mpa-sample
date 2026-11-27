# JSON API (`/api/v1`)

The machine-facing JSON surface of zatudan. For the app overview see
[../README.md](../README.md); for the HTML routes and web features see
[ui.md](ui.md); for consolidated UI rules see [ja/ui-rules.md](ja/ui-rules.md).

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
| GET/POST | `/api/v1/workspaces`                        | List / create workspaces                            |
| GET/DELETE | `/api/v1/workspaces/{slug}`               | Read / delete a workspace                           |
| GET    | `/api/v1/w/{ws}/issues`                       | List: `?status=&q=&label=<name>&sort=&page=&per_page=` (cap 100) |
| POST   | `/api/v1/w/{ws}/issues`                       | Create `{title, body, label_ids\|labels}` → 201     |
| GET    | `/api/v1/w/{ws}/issues/{id}`                  | Issue + labels + comments in one call               |
| PATCH  | `/api/v1/w/{ws}/issues/{id}`                  | Partial update (only provided fields change)        |
| POST   | `/api/v1/w/{ws}/issues/{id}/status`           | `{status: "open"\|"closed"}`                      |
| DELETE | `/api/v1/w/{ws}/issues/{id}`                  | Delete → 204                                        |
| POST   | `/api/v1/w/{ws}/issues/{id}/comments`         | `{body}` → 201                                      |
| GET    | `/api/v1/w/{ws}/issues/{id}/markdown`         | Issue + comments as one `text/markdown` document    |
| GET/POST | `/api/v1/w/{ws}/labels`                     | All labels / create (`PATCH`/`DELETE` on `…/labels/{id}`) |
| GET    | `/api/v1/w/{ws}/discussions`                  | List: `?category=&answered=&q=&sort=&page=`         |
| POST   | `/api/v1/w/{ws}/discussions`                  | `{title, body, category_id\|category}` → 201        |
| GET    | `/api/v1/w/{ws}/discussions/{id}`             | Discussion + threads (replies nested)               |
| PATCH  | `/api/v1/w/{ws}/discussions/{id}`             | Partial update                                      |
| DELETE | `/api/v1/w/{ws}/discussions/{id}`             | Delete → 204                                        |
| GET    | `/api/v1/w/{ws}/discussions/{id}/markdown`    | Discussion + threads + replies as `text/markdown`   |
| POST   | `/api/v1/w/{ws}/discussions/{id}/threads`     | `{body}` → 201 (new thread)                         |
| POST   | `/api/v1/w/{ws}/discussions/{id}/threads/{tid}/comments` | `{body}` → 201 (reply to thread)          |
| POST   | `/api/v1/w/{ws}/discussions/{id}/threads/{tid}/answer` | `{answer: bool}` mark/unmark the answer (qa) |
| GET    | `/api/v1/discussion-categories`               | All categories                                      |
| GET/POST | `/api/v1/pulls`                         | PR list (`?status=&q=&sort=&page=`) / create `{base, head, title, body}` (repo-gated) |
| GET/PATCH/DELETE | `/api/v1/pulls/{id}`          | Read / partial update / delete                    |
| GET/POST | `/api/v1/pulls/{id}/comments`           | List / create (`{body}`, `path`+`line`+`side` anchors to a diff line) |
| PATCH/DELETE | `/api/v1/pulls/{id}/comments/{cid}`  | Edit / delete a comment                           |
| POST   | `/api/v1/pulls/{id}/comments/{cid}/hide` + `…/unhide` | Reasoned minimize/restore         |
| POST   | `/api/v1/pulls/{id}/status`                | `{status: "open"\|"closed"}`                    |
| POST   | `/api/v1/pulls/{id}/merge`                 | `{method: "merge"\|"squash"\|"rebase", message?}` |
| GET    | `/api/v1/pulls/{id}/diff`                  | Unified diff as `text/plain`; `X-Truncated: true` means the patch hit the server output cap |
| GET    | `/api/v1/pulls/{id}/markdown`              | PR + commits + review comments as `text/markdown` |

Notes:

- `{ws}` is the workspace slug — resources outside `/api/v1/w/{ws}` are
  the two global top-level rows (workspaces, discussion categories) and
  the repo-scoped `/api/v1/pulls*` tree, which is only mounted when a
  git repo is resolved.
- Responses are `application/json`; errors use
  `{"error":{"message": ..., "fields": {...}}}` (`fields` only for
  per-field 422s).
- Timestamps are RFC 3339 strings. Status changes stay a `POST` action
  endpoint, mirroring the HTML app's action routes.
- The `/markdown` endpoints answer `text/markdown; charset=utf-8` with
  `Content-Disposition: inline` — the same document the web twins serve,
  rendered by `internal/export`.
