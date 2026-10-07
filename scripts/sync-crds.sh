#!/usr/bin/env bash
# Vendor each CRD listed in scripts/crds-upstream.txt from its service repo at
# the pinned commit into <chart dir>/crds/. With --check, write nothing and
# fail if any vendored copy differs from the pinned upstream file.
# The controller-gen version annotation is dropped from every copy: it is
# build metadata only, and its host name trips the identifier scrub.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
check=false
case "${1:-}" in
  --check) check=true ;;
  "") ;;
  *) echo "usage: $0 [--check]" >&2; exit 2 ;;
esac

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

status=0
while read -r dir repo commit path; do
  case "$dir" in ''|'#'*) continue ;; esac
  dest="$root/$dir/crds/$(basename "$path")"
  url="https://raw.githubusercontent.com/$repo/$commit/$path"
  echo "fetch $repo@${commit:0:12} $path"
  curl -fsSL --retry 3 -o "$tmp/raw.yaml" "$url"
  awk '
    /^    controller-gen\.kubebuilder\.io\/version:/ { next }
    held { if ($0 !~ /^    /) { held = 0 } else { print "  annotations:"; held = 0 } }
    /^  annotations:$/ { held = 1; next }
    { print }
  ' "$tmp/raw.yaml" > "$tmp/crd.yaml"
  if $check; then
    if ! diff -u "$dest" "$tmp/crd.yaml"; then
      echo "::error::$dir: vendored CRD differs from $repo@$commit:$path; run scripts/sync-crds.sh" >&2
      status=1
    fi
  else
    mkdir -p "$(dirname "$dest")"
    cp "$tmp/crd.yaml" "$dest"
    echo "wrote ${dest#"$root"/}"
  fi
done < "$root/scripts/crds-upstream.txt"
exit "$status"
