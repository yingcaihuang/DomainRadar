#!/usr/bin/env bash
# Runs ESLint on staged frontend files.
# pre-commit passes file paths relative to the repo root; this script strips
# the frontend directory prefix and runs eslint from within that directory so
# its flat config (eslint.config.*) is resolved correctly.
#
# Adjust FRONTEND_DIR to match your repository layout.
set -euo pipefail

FRONTEND_DIR="frontend"
PREFIX="${FRONTEND_DIR}/"

# Collect args, stripping the frontend prefix from each path.
files=()
for f in "$@"; do
  files+=("${f#"$PREFIX"}")
done

# Nothing to lint (shouldn't happen given the `files:` filter, but be safe).
if [ "${#files[@]}" -eq 0 ]; then
  exit 0
fi

cd "$FRONTEND_DIR"
exec npx eslint --fix "${files[@]}"
