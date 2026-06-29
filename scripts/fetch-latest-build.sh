#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  scripts/fetch-latest-build.sh [--repo owner/name] [--stable] [--download-only] [-- <firescribe args>]

Options:
  --repo           GitHub repo in owner/name format. Auto-detected by default.
  --stable         Download latest stable release (non-prerelease).
  --download-only  Download and verify only, do not run the binary.
  -h, --help       Show this help.

Notes:
  - Requires GitHub CLI (`gh`) and login: gh auth login
  - Default behavior downloads the fixed dev prerelease tag.
EOF
}

fail() {
  echo "error: $*" >&2
  exit 1
}

repo=""
stable=0
download_only=0
app_args=()

while [[ $# -gt 0 ]]; do
  case "$1" in
    --repo)
      shift
      [[ $# -gt 0 ]] || fail "--repo requires a value"
      repo="$1"
      ;;
    --stable)
      stable=1
      ;;
    --download-only)
      download_only=1
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    --)
      shift
      app_args+=("$@")
      break
      ;;
    *)
      app_args+=("$1")
      ;;
  esac
  shift
done

command -v gh >/dev/null 2>&1 || fail "gh CLI not found. Install: https://cli.github.com/"
gh auth status >/dev/null 2>&1 || fail "gh CLI not logged in. Run: gh auth login"

if [[ -z "$repo" ]]; then
  repo="$(gh repo view --json nameWithOwner --jq '.nameWithOwner' 2>/dev/null || true)"
fi

if [[ -z "$repo" ]]; then
  remote="$(git config --get remote.origin.url 2>/dev/null || true)"
  case "$remote" in
    git@github.com:*) repo="${remote#git@github.com:}" ;;
    https://github.com/*) repo="${remote#https://github.com/}" ;;
    http://github.com/*) repo="${remote#http://github.com/}" ;;
    ssh://git@github.com/*) repo="${remote#ssh://git@github.com/}" ;;
  esac
  repo="${repo%.git}"
fi

[[ -n "$repo" ]] || fail "unable to detect repo, pass --repo owner/name"

os="$(uname -s)"
arch="$(uname -m)"
target=""
ext=""

case "$os" in
  Darwin)
    case "$arch" in
      arm64|aarch64) target="darwin-arm64" ;;
      x86_64|amd64) target="darwin-amd64" ;;
      *) fail "unsupported macOS architecture: $arch" ;;
    esac
    ;;
  Linux)
    case "$arch" in
      x86_64|amd64) target="linux-amd64" ;;
      aarch64|arm64) target="linux-arm64" ;;
      armv7l|armv7) target="linux-armv7" ;;
      riscv64) target="linux-riscv64" ;;
      *) fail "unsupported Linux architecture: $arch" ;;
    esac
    ;;
  MINGW*|MSYS*|CYGWIN*)
    ext=".exe"
    case "$arch" in
      x86_64|amd64) target="windows-amd64" ;;
      arm64|aarch64) target="windows-arm64" ;;
      *) fail "unsupported Windows architecture: $arch" ;;
    esac
    ;;
  *)
    fail "unsupported OS: $os"
    ;;
esac

if [[ "$stable" -eq 1 ]]; then
  mode="stable"
else
  mode="dev"
fi

echo "repo:    $repo"
echo "branch:  master/main"
echo "mode:    $mode"
echo "target:  $target"

if [[ "$stable" -eq 1 ]]; then
  echo "release select: latest stable release (no build wait)"
  tag="$(gh api "/repos/${repo}/releases?per_page=100" --jq '[.[] | select(.draft == false and .prerelease == false) | .tag_name][0] // empty')"
else
  echo "release select: fixed dev prerelease tag"
  tag="dev"
fi

[[ -n "$tag" ]] || fail "no matching release found for repo: $repo"

bin_name="firescribe-${target}${ext}"
sha_name="${bin_name}.sha256"
run_dir="$(pwd)/test/${target}/${tag}"
mkdir -p "$run_dir"

echo "release: $tag"
echo "dir:     $run_dir"

gh release download "$tag" \
  --repo "$repo" \
  --pattern "$bin_name" \
  --pattern "$sha_name" \
  --dir "$run_dir" \
  --clobber

cd "$run_dir"

expected="$(grep -Eo '[0-9a-fA-F]{64}' "$sha_name" | head -n1 | tr 'A-F' 'a-f')"
[[ -n "$expected" ]] || fail "invalid sha256 file: $sha_name"

if command -v sha256sum >/dev/null 2>&1; then
  actual="$(sha256sum "$bin_name" | awk '{print $1}')"
elif command -v shasum >/dev/null 2>&1; then
  actual="$(shasum -a 256 "$bin_name" | awk '{print $1}')"
else
  actual=""
  echo "warning: no sha256 verifier found; skipped checksum verification"
fi
if [[ -n "$actual" ]]; then
  [[ "$expected" == "$actual" ]] || fail "sha256 mismatch"
  echo "${bin_name}: OK"
fi

if [[ "$bin_name" != *.exe ]]; then
  chmod +x "$bin_name"
fi

if [[ "$download_only" -eq 1 ]]; then
  echo "download complete: $run_dir/$bin_name"
  exit 0
fi

echo "starting: $run_dir/$bin_name ${app_args[*]:-}"
if [[ ${#app_args[@]} -gt 0 ]]; then
  "./$bin_name" "${app_args[@]}"
else
  "./$bin_name"
fi
