---
name: storybook
description: Frontend verification workflow for go-classic-mpa-sample. Use whenever a task changes how a page renders — editing `internal/web/templates/` or `internal/web/static/`, touching `internal/web/story_pages.go`, adding or modifying a handler/page in `internal/web`, or when a visual/interactive check is requested — to iterate against the repo's homemade storybook (hot-reload, variant grids, HTTP replay) instead of booting the real app.
---

# Storybook-driven frontend verification

This repo ships a homemade storybook (`cmd/storybook`, built on
`github.com/podhmo/minigo/storybook`). Frontend changes — templates,
`static/app.css`, page view-models — should be tried out here **before**
booting the real app: it renders pages with fake data instantly, groups a
page's data-state variants into one grid, and hot-reloads every edit.

## Start it

```sh
make storybook    # serves internal/web stories on http://127.0.0.1:6006
```

It auto-runs `make setup-minigo` (clones `podhmo/minigo` into
`third_party/`, a nested module `cmd/storybook` replaces it against).
Keep the process running for the whole task — every source read happens
per request, so:

- editing `internal/web/templates/*.html` → reload the browser, done
- editing `internal/web/story_pages.go` (fixtures, stories, `storyStore`) → reload, done
- editing `internal/web/static/*` → served live under `/static/`, reload, done
- editing any other `internal/web/*.go` (renderer, view-models) → reload, done

A template parse error or a `{{.MissingField}}` shows up as the story
rendering **500**, not silently — treat that as the failure signal.

## What the index shows

- **Print stories** (`internal/web/story_pages.go`, `// +build story`
  tag): each renders one `<html` page with fake data. They are grouped
  under the render helper they end with (`renderIssueList`,
  `renderIssueDetail`, `renderIssueForm`, `renderIssueDelete`,
  `renderError`) — each group name links to `/grid/<name>`, all variants
  side by side.
- **Handler stories** (`StoryHttp*`): replay a real request through the
  app's mux against the fake `storyStore` — recorded status AND headers
  (CSP `script-src 'none'` included). They open in a new tab.
- **Viewport switcher**: `?w=320|360|768` narrows the iframe — use it to
  check responsive behavior without resizing a window.

## Verifying a frontend change — checklist

1. `make storybook` (once per task).
2. Open `http://127.0.0.1:6006`. In a headless context use
   `curl -sf http://127.0.0.1:6006/story/<StoryName>` / playwright-cli
   (see `.claude/skills/playwright-cli`); screenshots belong in the PR
   when the change is visual.
3. Pick the right story:
   - Changed an existing page? Open its story (e.g. `?s=StoryIssueList`),
     then its `/grid/<renderFunc>` to check all data states at once.
   - New data state (empty, error, paged, …)? Add a print story in
     `story_pages.go` ending on the existing `renderXxx(status, data)`
     helper — the index picks it up immediately, grouped with its
     siblings.
   - New page/handler? Add a `renderXxx` helper + a `StoryXxx`, and a
     `StoryHttpXxx` if the route itself matters (status, headers).
4. Check the variants grid at `?w=360` for narrow-viewport regressions.
5. For headers/status replay, open the `StoryHttp*` link (new tab) —
   `curl -I` shows the real status + CSP.
6. Regression safety net: `cd cmd/storybook && go test ./...` asserts
   corpus-wide invariants (every discovered story renders, groups don't
   collapse, statuses/CSP replay). No test edits needed when adding
   stories — the discovered corpus is the spec.

## Export (static snapshot)

```sh
cd cmd/storybook && go run . -o /tmp/book ../../internal/web
python3 -m http.server -d /tmp/book    # index.html + grid_*.html + static/
```

Same content as the live server, minus hot-reload. Useful for attaching
a snapshot to a PR.

## Hard rules

- Story files in `internal/web` carry `//go:build story` — they never
  ship in the binary; keep `story_pages.go` for story data and helpers.
- A print story must end on an unqualified `renderXxx(...)` call — the
  group name is derived from it (a story ending on `printRec(...)`
  collapses the whole corpus into one group).
- Stories and fixtures live in `story_pages.go` and are pure script —
  no restarts, no recompiles; if you find yourself restarting the
  storybook after an edit, something is wrong.
- JavaScript is intentionally absent (`script-src 'none'`); verify
  behavior with server-rendered markup + CSS only.
