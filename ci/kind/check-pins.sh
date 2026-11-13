#!/usr/bin/env bash
# Compare every commit pinned in ci/kind/images.txt with the head of that
# repo's main branch. Fails (exit 1) when any pin is behind, so the kind
# install test cannot silently drift from the service code the chart ships
# with. With --update it rewrites the pins to those heads instead; the bump
# is then an ordinary PR (see ci/kind/README.md).
#
#   ci/kind/check-pins.sh            # report drift, exit 1 if any
#   ci/kind/check-pins.sh --update   # move every pin to its repo's main head
#
# Environment (for tests): IMAGES_FILE overrides the pin file, REPO_BASE the
# git URL prefix (default https://github.com), BRANCH the branch (default main).
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
file="${IMAGES_FILE:-$here/images.txt}"
base="${REPO_BASE:-https://github.com}"
branch="${BRANCH:-main}"
mode=check
case "${1:-}" in
  "") ;;
  --update) mode=update ;;
  *) echo "usage: $0 [--update]" >&2; exit 2 ;;
esac

log() { echo "check-pins: $*" >&2; }

# One lookup per repo, however many aliases share it (a plain cache dir, so
# this also runs on bash 3.2).
cache="$(mktemp -d)"
head_of() {
  local repo="$1" key sha
  key="$cache/${repo//\//_}"
  if [ ! -s "$key" ]; then
    log "looking up $repo $branch"
    sha=$(git ls-remote "$base/$repo.git" "refs/heads/$branch" | awk 'NR == 1 { print $1 }')
    if [ -z "$sha" ]; then
      log "no $branch head found for $repo"
      return 1
    fi
    printf '%s\n' "$sha" > "$key"
  fi
  cat "$key"
}

drift=0
out="$(mktemp)"
trap 'rm -rf "$out" "$cache"' EXIT
while IFS= read -r line || [ -n "$line" ]; do
  case "$line" in
    '#'*|'') printf '%s\n' "$line" >> "$out"; continue ;;
  esac
  read -r alias_name repo commit rest <<<"$line"
  if ! now=$(head_of "$repo"); then
    drift=1
    printf '%s\n' "$line" >> "$out"
    continue
  fi
  if [ "$commit" = "$now" ]; then
    log "ok      $alias_name $repo ${commit:0:12}"
    printf '%s\n' "$line" >> "$out"
  else
    drift=1
    log "BEHIND  $alias_name $repo pinned ${commit:0:12}, $branch is ${now:0:12}"
    # Keep the original column layout: swap the commit, keep the rest.
    printf '%s\n' "${line/$commit/$now}" >> "$out"
  fi
done < "$file"

if [ "$mode" = update ]; then
  cp "$out" "$file"
  log "pins rewritten in $file"
  exit 0
fi
if [ "$drift" -ne 0 ]; then
  log "pins are behind main; run ci/kind/check-pins.sh --update and open a PR"
  echo "::error::ci/kind/images.txt pins are behind the services' main (run ci/kind/check-pins.sh --update)" >&2
  exit 1
fi
log "all pins are current"
