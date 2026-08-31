#!/usr/bin/env bash
# Bootstraps the standard multi-language pre-commit setup in the current repo.
#
# Usage:
#   install.sh [TARGET_REPO_DIR] [--yes]
#
# If TARGET_REPO_DIR is omitted, the current working directory is used.
# Must be run from (or given) a git repository root.
#
# This script may install THIRD-PARTY software (the `pre-commit` tool) if it is
# not already present. By default it asks for confirmation before doing so. To
# skip the prompt (e.g. in CI), pass `--yes` or set PRECOMMIT_SETUP_YES=1.
set -euo pipefail

# ---------------------------------------------------------------------------
# Parse arguments
# ---------------------------------------------------------------------------
ASSUME_YES="${PRECOMMIT_SETUP_YES:-0}"
TARGET_DIR=""
for arg in "$@"; do
  case "$arg" in
    --yes|-y)
      ASSUME_YES=1
      ;;
    *)
      if [ -z "$TARGET_DIR" ]; then
        TARGET_DIR="$arg"
      fi
      ;;
  esac
done

# ---------------------------------------------------------------------------
# Resolve paths
# ---------------------------------------------------------------------------
SKILL_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TARGET_DIR="${TARGET_DIR:-$(pwd)}"

# Ask for confirmation unless --yes/-y or PRECOMMIT_SETUP_YES=1 was given.
# Returns 0 if the user agrees, 1 otherwise.
confirm() {
  local prompt="$1"
  if [ "$ASSUME_YES" = "1" ]; then
    return 0
  fi
  if [ ! -t 0 ]; then
    # No interactive terminal and no --yes: refuse to install silently.
    echo "ERROR: $prompt" >&2
    echo "       Non-interactive session detected. Re-run with --yes to proceed." >&2
    return 1
  fi
  printf '%s [y/N] ' "$prompt" >&2
  read -r reply
  case "$reply" in
    [yY]|[yY][eE][sS]) return 0 ;;
    *) return 1 ;;
  esac
}

cd "$TARGET_DIR"

if [ ! -d .git ]; then
  echo "ERROR: $TARGET_DIR is not a git repository root (.git not found)." >&2
  echo "Run this from your repo root, or pass the repo path as an argument." >&2
  exit 1
fi

echo "==> Target repository: $TARGET_DIR"

# ---------------------------------------------------------------------------
# 1. Ensure pre-commit is installed
# ---------------------------------------------------------------------------
if ! command -v pre-commit >/dev/null 2>&1; then
  # Pick the installer that is available on this machine.
  if command -v pipx >/dev/null 2>&1; then
    INSTALL_CMD="pipx install pre-commit"
  elif command -v pip3 >/dev/null 2>&1; then
    INSTALL_CMD="pip3 install --user pre-commit"
  elif command -v pip >/dev/null 2>&1; then
    INSTALL_CMD="pip install --user pre-commit"
  elif command -v brew >/dev/null 2>&1; then
    INSTALL_CMD="brew install pre-commit"
  else
    echo "ERROR: could not find pipx/pip/brew to install pre-commit." >&2
    echo "Install it manually: https://pre-commit.com/#install" >&2
    exit 1
  fi

  echo "==> 'pre-commit' is not installed on this system."
  echo "    This is third-party software and will be installed with:"
  echo "        $INSTALL_CMD"
  if ! confirm "Install pre-commit now?"; then
    echo "Aborted. Install pre-commit manually, then re-run this script:" >&2
    echo "  https://pre-commit.com/#install" >&2
    exit 1
  fi
  echo "==> Installing pre-commit..."
  eval "$INSTALL_CMD"
else
  echo "==> pre-commit already installed: $(pre-commit --version)"
fi

# ---------------------------------------------------------------------------
# 2. Copy config template and helper script (don't overwrite without asking)
# ---------------------------------------------------------------------------
if [ -f .pre-commit-config.yaml ]; then
  echo "==> .pre-commit-config.yaml already exists — leaving it untouched."
  echo "    Template available at: $SKILL_DIR/assets/pre-commit-config.yaml"
else
  cp "$SKILL_DIR/assets/pre-commit-config.yaml" .pre-commit-config.yaml
  echo "==> Copied .pre-commit-config.yaml to repo root."
fi

mkdir -p scripts
if [ -f scripts/eslint-staged.sh ]; then
  echo "==> scripts/eslint-staged.sh already exists — leaving it untouched."
else
  cp "$SKILL_DIR/scripts/eslint-staged.sh" scripts/eslint-staged.sh
  chmod +x scripts/eslint-staged.sh
  echo "==> Copied scripts/eslint-staged.sh (adjust FRONTEND_DIR if needed)."
fi

# ---------------------------------------------------------------------------
# 3. Register git hooks
# ---------------------------------------------------------------------------
echo "==> Installing git hooks..."
pre-commit install
pre-commit install --hook-type commit-msg

# ---------------------------------------------------------------------------
# 4. Validate config
# ---------------------------------------------------------------------------
echo "==> Validating config..."
pre-commit validate-config

echo ""
echo "============================================================"
echo "Done. Next steps:"
echo "  1. Review .pre-commit-config.yaml and adjust the ESLint"
echo "     'files:' path prefix + scripts/eslint-staged.sh FRONTEND_DIR"
echo "     to match your repo layout."
echo "  2. Run a full check:  pre-commit run --all-files"
echo "============================================================"
