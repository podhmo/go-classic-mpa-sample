---
name: zatu-cli
description: Work with this repo's issue tracker from the terminal via `zatu`, the gh-style CLI in cmd/zatu — searching/listing/viewing issues and discussions, filing new ones, adding progress comments, correcting bodies, changing state, managing labels/workspaces. Use whenever a task records progress or asks questions on the tracker instead of editing the DB or driving the web UI.
---

# zatu — the tracker's gh-style CLI

`zatu` (`cmd/zatu`) is the supported way to read and write this app's
issues/discussions from a shell. **Usage is fully discoverable via
`--help` — consult it rather than guessing flags:**

```sh
go run ./cmd/zatu --help           # command surface + backend selection
go run ./cmd/zatu issue --help     # one resource group's actions
go run ./cmd/zatu issue list --help  # one action's arguments and flags
```

For a session of many calls, build once (`go build -o /tmp/zatu
./cmd/zatu`) instead of paying `go run` per invocation.

Facts not covered by `--help`:

- Local mode reads `zatudan.db` directly (`--db`, env `ZATUDAN_DB`);
  `--server`/`--token` switch to remote mode over `/api/v1`, where every
  write is attributed to the token's user (local mode writes are
  anonymous — use remote mode when the author name matters).
- A brand-new DB has no discussion categories until
  `go run ./cmd/zatudan -db PATH init` runs once (labels and the default
  workspace come with the schema itself).
- `tools/demo-history.sh` replays a full example history through the CLI
  (issues, progress comments, corrections, closes, discussions, answer
  marks) — a runnable reference for the usual flows.
