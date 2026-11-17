---
name: verify-ui
description: Use after a UI change on go-classic-mpa-sample when the homemade storybook is no longer enough and the change must be exercised in the real running app end-to-end — real routing/middleware, the real SQLite store, PRG redirects, view transitions across navigations, JavaScript event behavior, or console/CSP errors — by driving a real browser with playwright-cli against `go run .` on :8080.
---

# Verify UI changes against the real app

The homemade storybook (see the `iterate-ui` skill) is the iteration
loop — but it renders pages against fake data and cannot prove what
only the real app has: the actual mux and middleware chain, the SQLite
store, POST→Redirect→GET flows, cross-document view transitions on
real navigations, and the real CSP response headers. When a change
needs that level of proof, run the app and drive a real browser.

## Start the app

```sh
go run .                    # http://127.0.0.1:8080, db: zatudan.db
go run . -db /tmp/e2e.db    # scratch DB — demo issues auto-seed on first launch
```

`SeedDemoData` runs on every startup and is a no-op on a non-empty
table, so a fresh `-db` file gives you demo issues, comments and labels
with zero setup. The `init` subcommand exists for schema maintenance
but is **not** needed to run the app.

## Drive the browser with playwright-cli

Command-level usage lives in `.claude/skills/playwright-cli`
(`pnpm dlx @playwright/cli` when the global command is absent). The
usual session shape:

```bash
playwright-cli open http://127.0.0.1:8080/
playwright-cli snapshot            # refs for click/fill targets
playwright-cli click e15
playwright-cli fill e5 "title" --submit
playwright-cli console             # JS errors / CSP violations — must stay empty
playwright-cli screenshot --filename=after-change.png
```

On this box a `.playwright/cli.config.json` pinning the chromium
executable may be required (see `docs/ja/pitfalls.md`); the repo
blueprint generates it at session start.

## What to check

Pick from this list what the change actually touches — the storybook
already covers markup/CSS variants, so this pass is about **behavior**:

- **The changed flow end-to-end**: submit the real form, follow the
  PRG redirect, confirm the flash/updated list renders from the store.
- **JavaScript (when present — policy in `docs/ja/decisions.md`)**:
  events fire and mutate the DOM as intended; JS is only allowed
  because this skill makes it verifiable. `console` must show no
  errors.
- **CSP**: `console` and `requests` show no blocked resources. Inline
  `<script>` is blocked regardless of the current `script-src`
  policy — inline JS is the classic failure; check the served
  directive in `internal/web/web.go`.
- **View transitions / CSS-only behavior**: click a real navigation
  (not `goto`) and confirm the transition/range styling engages.
- **Viewport**: `playwright-cli resize 360 800` for the narrow layout
  the storybook `?w=360` only approximates.
- **Regression**: `go test ./...` still covers server-side behavior;
  this skill is the visual/interactive complement, not a replacement.

## Hard rules

- Verify against the real server, not the storybook, when the question
  is "does it work in the app" — storybook fakes the store and adds
  its own chrome around the page.
- A clean `playwright-cli console` is part of done for any JS- or
  CSP-touching change; a blocked-script violation means the change
  does not work in production either.
- Screenshots/short videos of the verified flow belong on the PR for
  visual changes (`gh pr comment --attach`, see
  `.claude/skills/playwright-cli/references/pr-attachments.md`).
