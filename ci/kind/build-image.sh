#!/usr/bin/env bash
# Build one service image for the kind install test from the repo and commit
# pinned in ci/kind/images.txt, tagged steward-ci/<alias>:<commit12>.
#
#   ci/kind/build-image.sh <alias> [workdir]
#
# With SAVE_DIR set, also writes the image to $SAVE_DIR/<alias>.tar for
# `kind load image-archive`.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
alias_name="${1:?usage: $0 <alias> [workdir]}"
work="${2:-$(mktemp -d)}"

read -r repo commit dockerfile < <(awk -v a="$alias_name" '$1 == a { print $2, $3, $4 }' "$here/images.txt")
if [ -z "${repo:-}" ]; then
  echo "no alias $alias_name in $here/images.txt" >&2
  exit 2
fi
tag="steward-ci/$alias_name:${commit:0:12}"
src="$work/$alias_name"

echo "build $tag from $repo@$commit ($dockerfile)"
rm -rf "$src"
mkdir -p "$src"
git -C "$src" init -q
git -C "$src" fetch -q --depth 1 "https://github.com/$repo.git" "$commit"
git -C "$src" checkout -q FETCH_HEAD
start=$(date +%s)
# BUILDER names a buildx builder to use instead of the default one (its own
# cache, which can be pruned without touching anything else's).
build=(docker build)
if [ -n "${BUILDER:-}" ]; then build=(docker buildx build --builder "$BUILDER" --load); fi
"${build[@]}" -q -f "$src/$dockerfile" -t "$tag" \
  --build-arg VERSION="ci-${commit:0:12}" --build-arg COMMIT="$commit" "$src" >/dev/null
echo "built $tag in $(( $(date +%s) - start ))s"
if [ -n "${SAVE_DIR:-}" ]; then
  mkdir -p "$SAVE_DIR"
  docker save -o "$SAVE_DIR/$alias_name.tar" "$tag"
  echo "saved $SAVE_DIR/$alias_name.tar"
fi
