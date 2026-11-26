#!/bin/sh
# tools/visual-diff.sh — screenshot every storybook story and pixel-diff
# it against a saved baseline, so a CSS/HTML change can be proved to have
# moved no pixels before it is committed.
#
#   tools/visual-diff.sh update [light|dark|all]   # save the current rendering as the baseline
#   tools/visual-diff.sh check  [light|dark|all]   # capture and compare against the baseline
#
# The mode defaults to all (or the whitespace-separated MODES list), so
# light and dark baselines are kept separately. Captures also include the
# State* shots: dialog open, copy feedback, and a toast driven on the
# storybook detail page.
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
PW_SESSION="${PW_SESSION:-visual-diff}"
STATE_SHOTS="StateDialogOpen StateCopyPermalink StateToast"
mkdir -p "$ROOT_DIR"
ROOT_DIR=$(cd "$ROOT_DIR" && pwd)

usage() {
	sed -n '2,18p' "$0"
	exit "${1:-1}"
}

mode_arg=${2:-${MODES:-all}}
if [ "$mode_arg" = "all" ]; then
	modes="light dark"
else
	modes=$mode_arg
fi
for mode in $modes; do
	case "$mode" in
		light|dark) ;;
		*) usage ;;
	esac
done

command -v playwright-cli >/dev/null || { echo "playwright-cli not on PATH (pnpm dlx @playwright/cli)" >&2; exit 1; }
command -v compare >/dev/null || { echo "ImageMagick 'compare' not found" >&2; exit 1; }
curl -sf "$STORYBOOK_URL/" -o /tmp/visual-diff-index.html || { echo "storybook not reachable at $STORYBOOK_URL — run \`make storybook\` first" >&2; exit 1; }

STORIES=$(grep -oE '/story/[A-Za-z0-9_-]+' /tmp/visual-diff-index.html | sed 's|^/story/||' | sort -u)
[ -n "$STORIES" ] || { echo "no /story/ links found on $STORYBOOK_URL/" >&2; exit 1; }

pwcli() {
	playwright-cli -s="$PW_SESSION" "$@"
}

close_browser() {
	pwcli close >/dev/null 2>&1 || true
}

capture() {
	out_dir=$1
	color_scheme=$2
	parent_dir=$(dirname "$out_dir")
	mkdir -p "$parent_dir"
	stage_dir=$(mktemp -d "$parent_dir/.capture.XXXXXX")
	tmp_js=$(mktemp --suffix=.js)
	# %STORIES% / %BASE% / %OUTDIR% / %SCHEME% are substituted below;
	# run-code only passes `page`, so everything else is baked into the file.
	cat > "$tmp_js" <<'EOF'
async page => {
  const stories = %STORIES%;
  const base = %BASE%;
  const outDir = %OUTDIR%;
  const colorScheme = %SCHEME%;
  await page.setViewportSize({width: 1280, height: 800});
  await page.emulateMedia({colorScheme});
  await page.context().grantPermissions(['clipboard-read', 'clipboard-write']).catch(() => {});
  const failed = [];

  async function settle() {
    await page.evaluate(() => document.fonts && document.fonts.ready);
  }
  async function prepare(story) {
    await page.goto(base + '/story/' + story, {waitUntil: 'load'});
    await settle();
    await page.evaluate(() => scrollTo(0, 0));
  }
  async function snap(name, fullPage) {
    // animations:'disabled' — otherwise live fades (e.g. the
    // .back-to-top scroll-state button) can land mid-flight and flake.
    await page.screenshot({path: outDir + '/' + name + '.png', fullPage, animations: 'disabled'});
  }

  for (const s of stories) {
    try {
      await prepare(s);
      await snap(s, true);
    } catch (e) { failed.push(s + ': ' + e); }
  }

  const scenarios = [
    ['StateDialogOpen', async () => {
      await prepare('StoryIssueDetail');
      await page.click('.delete-open');
      await page.waitForSelector('.confirm-dialog:modal');
      await snap('StateDialogOpen', false);
    }],
    ['StateCopyPermalink', async () => {
      await prepare('StoryIssueDetail');
      await page.click('.comment .copy-btn');
      await page.waitForSelector('.copy-btn[data-copied]', {timeout: 2000});
      await page.waitForFunction(() =>
        [...document.querySelectorAll('.copy-btn[data-copied]')]
          .some((button) => button.parentElement.querySelector('.copy-tip')?.textContent.trim())
      );
      await snap('StateCopyPermalink', false);
    }],
    ['StateToast', async () => {
      await prepare('StoryIssueDetail');
      await page.evaluate(async () => {
        (await import('/static/toast.js')).toast('Visual diff toast');
      });
      await page.waitForSelector('.toast:popover-open');
      await snap('StateToast', false);
    }],
  ];
  for (const [name, run] of scenarios) {
    try {
      await run();
    } catch (e) { failed.push(name + ': ' + e); }
  }
  return JSON.stringify(failed);
}
EOF
	STORIES_DATA=$STORIES python3 - "$tmp_js" "$STORYBOOK_URL" "$stage_dir" "$color_scheme" <<'PY'
import json
import os
import sys

path, base, outdir, scheme = sys.argv[1:]
with open(path) as f:
    source = f.read()
replacements = {
    "%STORIES%": json.dumps(os.environ["STORIES_DATA"].split()),
    "%BASE%": json.dumps(base),
    "%OUTDIR%": json.dumps(outdir),
    "%SCHEME%": json.dumps(scheme),
}
for placeholder, value in replacements.items():
    source = source.replace(placeholder, value)
with open(path, "w") as f:
    f.write(source)
PY
	# run-code expects an already-open browser. Start a fresh one per capture
	# so no state or tabs leak between modes.
	close_browser
	if ! pwcli open >/dev/null 2>&1; then
		rm -f "$tmp_js"
		rm -rf "$stage_dir"
		echo "could not open playwright browser; keeping existing $out_dir" >&2
		exit 1
	fi
	if ! pwcli run-code --filename "$tmp_js"; then
		close_browser
		rm -f "$tmp_js"
		rm -rf "$stage_dir"
		echo "capture failed; keeping existing $out_dir" >&2
		exit 1
	fi
	close_browser
	rm -f "$tmp_js"
	missing=""
	for story in $STORIES $STATE_SHOTS; do
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

compare_mode() {
	mode=$1
	base_dir="$ROOT_DIR/baseline/$mode"
	current_dir="$ROOT_DIR/current/$mode"
	diff_dir="$ROOT_DIR/diff/$mode"
	capture "$current_dir" "$mode"
	mkdir -p "$diff_dir"
	fail=0
	for png in "$base_dir"/*.png; do
		name=$(basename "$png")
		cur="$current_dir/$name"
		if [ ! -f "$cur" ]; then echo "MISSING  $mode/$name"; fail=1; continue; fi
		n=$(compare -metric AE "$png" "$cur" "$diff_dir/$name" 2>&1) || true
		case "$n" in
			''|*[!0-9]*) echo "ERROR    $mode/$name ($n)"; fail=1 ;;
			*)
				if [ "$n" -gt "$PIXEL_THRESHOLD" ]; then
					echo "DIFF $n  $mode/$name"; fail=1
				fi ;;
		esac
	done
	for png in "$current_dir"/*.png; do
		name=$(basename "$png")
		if [ ! -f "$base_dir/$name" ]; then
			echo "NEW      $mode/$name (not in baseline)"
			fail=1
		fi
	done
	[ "$fail" -eq 0 ] && echo "OK($mode): all shots within $PIXEL_THRESHOLD px"
	return "$fail"
}

case "${1:-}" in
	update)
		for mode in $modes; do
			capture "$ROOT_DIR/baseline/$mode" "$mode"
			echo "baseline updated in $ROOT_DIR/baseline/$mode"
		done
		;;
	check)
		missing=0
		for mode in $modes; do
			if [ ! -d "$ROOT_DIR/baseline/$mode" ]; then
				echo "no $mode baseline — run \`tools/visual-diff.sh update $mode\` first" >&2
				missing=1
			fi
		done
		[ "$missing" -eq 0 ] || exit 1
		fail=0
		for mode in $modes; do
			compare_mode "$mode" || fail=1
		done
		exit "$fail"
		;;
	*) usage ;;
esac
