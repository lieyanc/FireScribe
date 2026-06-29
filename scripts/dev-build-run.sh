#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  scripts/dev-build-run.sh [--output <path>] [--build-only] [-- <firescribe args>]

Options:
  -o, --output      Binary output path (default: <repo>/.bin/firescribe)
      --build-only  Build only, do not start the service
  -h, --help        Show this help
EOF
}

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUTPUT_PATH="$ROOT_DIR/.bin/firescribe"
BUILD_ONLY=0
EXTRA_ARGS=()

while [[ $# -gt 0 ]]; do
  case "$1" in
    -o|--output)
      shift
      [[ $# -gt 0 ]] || { echo "error: --output requires a value" >&2; exit 1; }
      OUTPUT_PATH="$1"
      ;;
    --build-only)
      BUILD_ONLY=1
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    --)
      shift
      EXTRA_ARGS=("$@")
      break
      ;;
    *)
      echo "error: unknown argument: $1" >&2
      usage
      exit 1
      ;;
  esac
  shift
done

mkdir -p "$(dirname "$OUTPUT_PATH")"

echo "[dev-run] building frontend"
(
  cd "$ROOT_DIR"
  npm --prefix web run build
  npm run stage:web
)

echo "[dev-run] building binary -> $OUTPUT_PATH"
(
  cd "$ROOT_DIR"
  go build -tags sqlite_fts5 -o "$OUTPUT_PATH" ./cmd/firescribe-server
)
echo "[dev-run] build complete"

if [[ "$BUILD_ONLY" -eq 1 ]]; then
  exit 0
fi

echo "[dev-run] starting service"
exec "$OUTPUT_PATH" "${EXTRA_ARGS[@]}"
