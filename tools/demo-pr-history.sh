#!/bin/sh
# demo-pr-history.sh — replay a small pull-request history into a zatu
# database using a real git repository and only the `zatu pr` CLI:
# two feature branches, review comments (one anchored to a diff line),
# a close and a merge.
#
# Usage:
#   tools/demo-pr-history.sh [DB] [REPO]
#
# Defaults: DB=zatudan.db, REPO=./.demo-pr-repo
#
# Browse the result with:
#   go run ./cmd/zatudan -db DB -repo REPO
#
# Notes:
# - The fixture repo is created when REPO has no .git. Re-running on an
#   existing repo keeps the branches as the last replay left them — a
#   merged head has an empty base..head range, so `pr create` fails and
#   the replay aborts. Delete REPO for a clean replay.
set -eu
cd "$(dirname "$0")/.."

DB="${1:-zatudan.db}"
REPO="${2:-.demo-pr-repo}"
mkdir -p "$REPO"
REPO="$(cd "$REPO" && pwd)"
export ZATUDAN_DB="$DB" ZATUDAN_REPO="$REPO"

go run ./cmd/zatudan -db "$DB" init >/dev/null

BIN="$(mktemp -d)/zatu"
go build -o "$BIN" ./cmd/zatu
zatu() { "$BIN" "$@"; }

export GIT_AUTHOR_NAME="demo" GIT_AUTHOR_EMAIL="demo@example.com"
export GIT_COMMITTER_NAME="demo" GIT_COMMITTER_EMAIL="demo@example.com"

if [ ! -d "$REPO/.git" ]; then
	git -C "$REPO" init -q -b main
	cat >"$REPO/app.txt" <<'EOF'
line one
line two
line three
EOF
	git -C "$REPO" add app.txt
	git -C "$REPO" commit -qm "initial commit"

	git -C "$REPO" checkout -qb feature/search
	cat >"$REPO/app.txt" <<'EOF'
line one
line two — search results shown here
line three
EOF
	cat >"$REPO/search.txt" <<'EOF'
search: match titles and bodies
EOF
	git -C "$REPO" add app.txt search.txt
	git -C "$REPO" commit -qm "add search snippet line"
	cat >>"$REPO/search.txt" <<'EOF'
render: show 3 lines of context
EOF
	git -C "$REPO" add search.txt
	git -C "$REPO" commit -qm "render context lines"
	git -C "$REPO" checkout -q main

	git -C "$REPO" checkout -qb feature/docs
	cat >"$REPO/README.txt" <<'EOF'
how to open a pull request
EOF
	git -C "$REPO" add README.txt
	git -C "$REPO" commit -qm "document the pr flow"
	git -C "$REPO" checkout -q main
fi

pr1=$(zatu pr create --base main --head feature/search \
	--title "Add search snippet view" \
	--body "Shows matching lines with context in the results page." | sed 's/.*#\([0-9][0-9]*\).*/\1/')
zatu pr comment create "$pr1" --path app.txt --line 2 \
	--body "Snippet line looks good; can we keep the dash separator?" >/dev/null
zatu pr comment create "$pr1" \
	--body "Rebased onto latest main; conflict badge should read clean." >/dev/null

pr2=$(zatu pr create --base main --head feature/docs \
	--title "Document the pr flow" \
	--body "Draft notes for contributors." | sed 's/.*#\([0-9][0-9]*\).*/\1/')
zatu pr close "$pr2" >/dev/null

zatu pr merge "$pr1" --method merge >/dev/null

zatu pr list --state all
echo "replay done — browse with: go run ./cmd/zatudan -db $DB -repo $REPO"
