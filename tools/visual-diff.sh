#!/bin/sh
# tools/visual-diff.sh — screenshot every storybook story and pixel-diff
# it against a saved baseline, so a CSS/HTML change can be proved to have
# moved no pixels before it is committed.
#
#   tools/visual-diff.sh update   # save the current rendering as the baseline
#   tools/visual-diff.sh check    # capture and compare against the baseline
#
# Prereqs: `make storybook` serving on 127.0.0.1:6006 (override with
# STORYBOOK_URL), playwright-cli on PATH (`pnpm dlx @playwright/cli`),
# and ImageMagick's `compare`. Captures live under .visual-diff/
# (gitignored). The pixel threshold defaults to 100 — sub-10px
# antialiasing noise is normal between runs.
set -eu
CDPATH=

STORYBOOK_URL="${STORYBOOK_URL:-http://127.0.0.1:6006}"
ROOT_DIR=.visual-diff
PIXEL_THRESHOLD="${PIXEL_THRESHOLD:-100}"
mkdir -p "$ROOT_DIR"
ROOT_DIR=$(cd "$ROOT_DIR" && pwd)

usage() {
	sed -n '2,14p' "$0"
	exit "${1:-1}"
}

command -v playwright-cli >/dev/null || { echo "playwright-cli not on PATH (pnpm dlx @playwright/cli)" >&2; exit 1; }
command -v compare >/dev/null || { echo "ImageMagick 'compare' not found" >&2; exit 1; }
curl -sf "$STORYBOOK_URL/" -o /tmp/visual-diff-index.html || { echo "storybook not reachable at $STORYBOOK_URL — run \`make storybook\` first" >&2; exit 1; }

STORIES=$(grep -oE '/story/[A-Za-z0-9_-]+' /tmp/visual-diff-index.html | sed 's|^/story/||' | sort -u)
[ -n "$STORIES" ] || { echo "no /story/ links found on $STORYBOOK_URL/" >&2; exit 1; }

capture() {
	out_dir=$1
	parent_dir=$(dirname "$out_dir")
	mkdir -p "$parent_dir"
	stage_dir=$(mktemp -d "$parent_dir/.capture.XXXXXX")
	tmp_js=$(mktemp --suffix=.js)
	# %STORIES% / %BASE% / %OUTDIR% are substituted below; run-code only
	# passes `page`, so everything else is baked into the file.
	cat > "$tmp_js" <<'EOF'
async page => {
  const stories = %STORIES%;
  const base = %BASE%;
  const outDir = %OUTDIR%;
  await page.setViewportSize({width: 1280, height: 800});
  const failed = [];
  for (const s of stories) {
    try {
      await page.goto(base + '/story/' + s, {waitUntil: 'load'});
      await page.evaluate(() => document.fonts && document.fonts.ready);
      // animations:'disabled' — otherwise live fades (e.g. the
      // .back-to-top scroll-state button) can land mid-flight and flake.
      await page.screenshot({path: outDir + '/' + s + '.png', fullPage: true, animations: 'disabled'});
    } catch (e) { failed.push(s + ': ' + e); }
  }
  return JSON.stringify(failed);
}
EOF
	STORIES_DATA=$STORIES python3 - "$tmp_js" "$STORYBOOK_URL" "$stage_dir" <<'PY'
import json
import os
import sys

path, base, outdir = sys.argv[1:]
with open(path) as f:
    source = f.read()
replacements = {
    "%STORIES%": json.dumps(os.environ["STORIES_DATA"].split()),
    "%BASE%": json.dumps(base),
    "%OUTDIR%": json.dumps(outdir),
}
for placeholder, value in replacements.items():
    source = source.replace(placeholder, value)
with open(path, "w") as f:
    f.write(source)
PY
	if ! playwright-cli run-code --filename "$tmp_js"; then
		rm -f "$tmp_js"
		rm -rf "$stage_dir"
		echo "capture failed; keeping existing $out_dir" >&2
		exit 1
	fi
	rm -f "$tmp_js"
	missing=""
	for story in $STORIES; do
		[ -f "$stage_dir/$story.png" ] || missing="$missing $story"
	done
	if [ -n "$missing" ]; then
		rm -rf "$stage_dir"
		echo "capture incomplete:$missing; keeping existing $out_dir" >&2
		exit 1
	fi
	backup_dir="$parent_dir/.backup.$$"
	if [ -d "$out_dir" ]; then
		mv "$out_dir" "$backup_dir"
	fi
	mv "$stage_dir" "$out_dir"
	if [ -d "$backup_dir" ]; then
		rm -rf "$backup_dir"
	fi
}

case "${1:-}" in
	update)
		capture "$ROOT_DIR/baseline"
		echo "baseline updated in $ROOT_DIR/baseline"
		;;
	check)
		[ -d "$ROOT_DIR/baseline" ] || { echo "no baseline — run \`tools/visual-diff.sh update\` first" >&2; exit 1; }
		capture "$ROOT_DIR/current"
		mkdir -p "$ROOT_DIR/diff"
		fail=0
		for png in "$ROOT_DIR"/baseline/*.png; do
			name=$(basename "$png")
			cur="$ROOT_DIR/current/$name"
			if [ ! -f "$cur" ]; then echo "MISSING  $name"; fail=1; continue; fi
			n=$(compare -metric AE "$png" "$cur" "$ROOT_DIR/diff/$name" 2>&1) || true
			case "$n" in
				''|*[!0-9]*) echo "ERROR    $name ($n)"; fail=1 ;;
				*)
					if [ "$n" -gt "$PIXEL_THRESHOLD" ]; then
						echo "DIFF $n  $name"; fail=1
					fi ;;
			esac
		done
		for png in "$ROOT_DIR"/current/*.png; do
			name=$(basename "$png")
			if [ ! -f "$ROOT_DIR/baseline/$name" ]; then
				echo "NEW      $name (not in baseline)"
				fail=1
			fi
		done
		[ "$fail" -eq 0 ] && echo "OK: all stories within $PIXEL_THRESHOLD px"
		exit "$fail"
		;;
	*) usage ;;
esac
