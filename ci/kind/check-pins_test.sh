#!/usr/bin/env bash
# Self-test for check-pins.sh against throwaway local git repos (no network).
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
t="$(mktemp -d)"
trap 'rm -rf "$t"' EXIT
who="12115015+Bugs5382@users.noreply.github.com"
export GIT_AUTHOR_NAME=Bugs5382 GIT_AUTHOR_EMAIL="$who" GIT_COMMITTER_NAME=Bugs5382 GIT_COMMITTER_EMAIL="$who"

mkrepo() { # <owner/repo>
  git init -q -b main "$t/$1.git"
  git -C "$t/$1.git" commit -q --allow-empty -m "chore: first"
}
mkrepo org/alpha
mkrepo org/beta
old_alpha=$(git -C "$t/org/alpha.git" rev-parse HEAD)
git -C "$t/org/alpha.git" commit -q --allow-empty -m "chore: second"
new_alpha=$(git -C "$t/org/alpha.git" rev-parse HEAD)
beta=$(git -C "$t/org/beta.git" rev-parse HEAD)

export REPO_BASE="$t" IMAGES_FILE="$t/images.txt"
cat > "$IMAGES_FILE" <<PINS
# comment kept
alpha org/alpha $old_alpha Dockerfile
alpha-x org/alpha $old_alpha render/Dockerfile render
beta org/beta $beta Dockerfile
PINS

fail() { echo "FAIL: $*" >&2; exit 1; }

if "$here/check-pins.sh" 2>/dev/null; then fail "drift not detected"; fi
echo "ok: drift is reported with a non-zero exit"

"$here/check-pins.sh" --update 2>/dev/null
grep -q "^# comment kept$" "$IMAGES_FILE" || fail "comment lost"
grep -q "^alpha org/alpha $new_alpha Dockerfile$" "$IMAGES_FILE" || fail "alpha not bumped"
grep -q "^alpha-x org/alpha $new_alpha render/Dockerfile render$" "$IMAGES_FILE" || fail "alpha-x not bumped"
grep -q "^beta org/beta $beta Dockerfile$" "$IMAGES_FILE" || fail "beta changed"
echo "ok: --update bumps only the stale pins and keeps columns and comments"

"$here/check-pins.sh" 2>/dev/null || fail "current pins reported as drift"
echo "ok: current pins pass"

if BRANCH=nope "$here/check-pins.sh" 2>/dev/null; then fail "missing branch passed"; fi
echo "ok: an unresolvable head fails"
