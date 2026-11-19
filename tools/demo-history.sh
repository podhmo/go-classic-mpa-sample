#!/bin/sh
# demo-history.sh — replay a realistic development history into a zatu
# database using only the `zatu` CLI: issues with progress comments,
# corrections (follow-up comments and `issue edit`), state changes,
# discussions with threads and an accepted answer.
#
# Usage:
#   tools/demo-history.sh [DB]        # default: zatudan.db
#
# Notes:
# - Master data (default workspace, labels, discussion categories) is
#   ensured via `zatudan -db DB init` at the start — a brand-new DB has
#   no discussion categories otherwise.
# - Re-running appends duplicates; point at a fresh DB for a clean replay.
set -eu
cd "$(dirname "$0")/.."

DB="${1:-zatudan.db}"
export ZATUDAN_DB="$DB"

go run ./cmd/zatudan -db "$DB" init >/dev/null

BIN="$(mktemp -d)/zatu"
go build -o "$BIN" ./cmd/zatu
zatu() { "$BIN" "$@"; }

# A workspace-scoped label created along the way, like a real project.
zatu label create "Needs Triage" --color "#fb8500" >/dev/null

zatu issue create --label Bug \
	--title "Search only matches issue title/body, not comments" \
	--body "Searching for a word that only appears inside a comment returns nothing.

Repro: comment 'only-in-comment' on an issue, then /issues?q=only-in-comment — no hits." >/dev/null
zatu comment create 1 --body "Reproduced. The list query LIKEs title and body; comments aren't joined at all." >/dev/null
zatu issue edit 1 --title "Search covers only issue title/body, not comments"
zatu comment create 1 --body "Correction to my first note: it's not a stale index — comments were never part of the search scope. Retitled the issue to say what is actually missing." >/dev/null
zatu comment create 1 --body "Draft up: filter now also matches comment bodies via EXISTS over the comments table." >/dev/null
zatu comment create 1 --body "Merged and verified on a seeded DB." >/dev/null
zatu issue close 1 >/dev/null

zatu issue create --label Enhancement \
	--title "Permalink anchor for each comment" \
	--body "Long issues are hard to reference — every comment should have a #cN anchor and a copy-link affordance." >/dev/null
zatu comment create 2 --body "Doing it CSS-only: id attributes + :target highlight, plus the copy-permalink button." >/dev/null
zatu issue close 2 >/dev/null

zatu issue create --label "Needs Triage" \
	--title "Pagination drops the active label filter" \
	--body "/issues?label=Bug shows filtered page 1, but page 2 comes back unfiltered — the label param is lost." >/dev/null
zatu comment create 3 --body "Looking into it. First guess was the pager not carrying query params." >/dev/null
zatu comment create 3 --body "Correction: the pager emits the params fine — the label <select> just isn't submitted on page change. Needs triage on whether to fix via hidden inputs or JS." >/dev/null

zatu issue create --label Documentation \
	--title "Document remote mode (ZATUDAN_SERVER / ZATUDAN_TOKEN)" \
	--body "The CLI's remote mode is only discoverable from --help today. README should cover server + token setup and 'zatu api'." >/dev/null
zatu comment create 4 --body "Added a remote-mode section to the README." >/dev/null
zatu issue close 4 >/dev/null

zatu issue create --label Feature \
	--title "'zatu issue edit' should accept --body-file" \
	--body "create takes --body-file but edit only takes --body — rewriting a long body from the CLI is painful." >/dev/null
zatu comment create 5 --body "Agreed, trivial parity gap. Pull requests welcome." >/dev/null

zatu issue create --title "Lable picker truncates long names" \
	--body "In the label picker, names over ~24 chars are clipped with no tooltip — accessibility nit." >/dev/null
zatu issue edit 6 --title "Label picker truncates long names" --label Bug
zatu comment create 6 --body "Fixed the title typo via 'zatu issue edit' (comments can't be edited — corrections go in follow-up comments or the issue body)." >/dev/null

zatu issue create --label Bug \
	--title "SSE reconnect loop after laptop sleep" \
	--body "After the machine wakes, the live-updates tab reconnects in a tight loop until reload. Needs a backoff." >/dev/null

zatu discussion create --category qa \
	--title "How do local 'zatu' writes reach a running server's live feed?" \
	--body "If I comment with 'zatu comment create' while the server is up, the open tab updates without a reload. What's the mechanism?" >/dev/null
zatu discussion comment create 1 --body "Every mutator records hints in the changes table inside its own tx; the server polls that table (WatchChanges) and republishes new rows to the SSE hub. Cross-process writes are covered by design." >/dev/null
zatu discussion answer 1 --thread 1 >/dev/null

zatu discussion create --category ideas \
	--title "A 'zatu issue status' overview command?" \
	--body "gh has 'issue status' (your open/assigned/mentioned). We have no accounts on local mode, so what would the equivalent be — recently-updated + your-workspace summary?" >/dev/null
T2=$(zatu discussion comment create 2 --body "Local mode is anonymous, so 'yours' has no meaning. Maybe 'status' = counts by state + recently updated instead?" | sed -n 's/created thread #\([0-9]*\).*/\1/p')
zatu discussion comment create 2 --thread "$T2" --body "Or skip status and lean on 'issue list --sort newest' — it already answers 'what moved lately'." >/dev/null

zatu discussion create --category general \
	--title "Naming: 'zatudan' for the server, 'zatu' for the CLI" \
	--body "One binary serves the app, the other is the gh-style client. Keeping the shared prefix seems clearer than two unrelated names." >/dev/null
zatu discussion comment create 3 --body "Agreed — same reason env vars share the ZATUDAN_ prefix regardless of which binary reads them." >/dev/null

zatu discussion create --category announcements \
	--title "zatu CLI: discussions and workspaces are now supported" \
	--body "'zatu discussion' and 'zatu workspace' round out the command surface; every level takes --help." >/dev/null

echo "replayed demo history into $DB"
