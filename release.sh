#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")" && pwd)"
cd "$REPO_ROOT"

CURRENT_BRANCH=$(git rev-parse --abbrev-ref HEAD)
REMOTE="origin"

# ── Helpers ──────────────────────────────────────────────────────────────────

red()    { printf "\033[0;31m%s\033[0m\n" "$1"; }
green()  { printf "\033[0;32m%s\033[0m\n" "$1"; }
yellow() { printf "\033[0;33m%s\033[0m\n" "$1"; }
bold()   { printf "\033[1m%s\033[0m\n" "$1"; }

usage() {
  cat <<EOF
Usage: ./release.sh <version>

  version   Semantic version to release (e.g. 1.3.0)
            The "v" prefix is added automatically.

Examples:
  ./release.sh 1.3.0        # tags v1.3.0 and pushes
  ./release.sh 2.0.0-beta   # tags v2.0.0-beta and pushes

What this script does:
  1. Validates the version format
  2. Checks for uncommitted changes
  3. Checks that the tag doesn't already exist
  4. Verifies the CHANGELOG has an entry for this version
  5. Creates an annotated git tag
  6. Pushes the tag to $REMOTE (triggers GitHub Actions release)
EOF
  exit 1
}

# ── Validation ───────────────────────────────────────────────────────────────

if [[ $# -lt 1 ]]; then
  usage
fi

VERSION="$1"
TAG="v${VERSION}"

if [[ ! "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[a-zA-Z0-9.]+)?$ ]]; then
  red "Error: Invalid version format '${VERSION}'"
  echo "  Expected: MAJOR.MINOR.PATCH or MAJOR.MINOR.PATCH-prerelease"
  echo "  Examples: 1.3.0, 2.0.0-beta, 1.4.0-rc.1"
  exit 1
fi

if [[ -n "$(git status --porcelain)" ]]; then
  red "Error: You have uncommitted changes."
  echo "  Commit or stash them before releasing."
  git status --short
  exit 1
fi

if git rev-parse "$TAG" >/dev/null 2>&1; then
  red "Error: Tag ${TAG} already exists."
  echo "  To delete and re-tag: git tag -d ${TAG} && git push ${REMOTE} :refs/tags/${TAG}"
  exit 1
fi

# ── Changelog check ──────────────────────────────────────────────────────────

if [[ -f "CHANGELOG.md" ]]; then
  if ! grep -q "## ${VERSION}" CHANGELOG.md; then
    yellow "Warning: No CHANGELOG.md entry found for version ${VERSION}."
    read -rp "  Continue anyway? [y/N] " confirm
    if [[ ! "$confirm" =~ ^[yY]$ ]]; then
      echo "Aborted."
      exit 1
    fi
  else
    green "CHANGELOG.md has entry for ${VERSION}"
  fi
fi

# ── Summary ──────────────────────────────────────────────────────────────────

echo ""
bold "Release Summary"
echo "  Tag:      ${TAG}"
echo "  Branch:   ${CURRENT_BRANCH}"
echo "  Remote:   ${REMOTE}"
echo "  Head:     $(git log -1 --format='%h %s')"
echo ""

read -rp "Create and push tag ${TAG}? [y/N] " confirm
if [[ ! "$confirm" =~ ^[yY]$ ]]; then
  echo "Aborted."
  exit 1
fi

# ── Tag and push ─────────────────────────────────────────────────────────────

git tag -a "$TAG" -m "Release ${TAG}"
green "Created annotated tag ${TAG}"

git push "$REMOTE" "$TAG"
green "Pushed ${TAG} to ${REMOTE}"

echo ""
bold "Done! GitHub Actions will now build and publish the release."
echo "  Track progress: https://github.com/MantisWare/crunchyroll-downloader/actions"
echo "  Release page:   https://github.com/MantisWare/crunchyroll-downloader/releases/tag/${TAG}"
