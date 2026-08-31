---
name: precommit-setup
description: Install and configure the standard multi-language pre-commit hooks (Python/Ruff, Node/TypeScript/ESLint, Rust/rustfmt+clippy, Go/gofmt+vet, Conventional Commits, plus general file checks). Use when adding pre-commit to a repository, setting up or standardizing git commit hooks, enforcing lint/format checks, or bootstrapping code-quality tooling across projects.
metadata:
  author: betty
  version: "1.0.0"
---

# Multi-language pre-commit setup

Bootstraps a consistent, multi-language pre-commit configuration in any git
repository. Language-specific hooks are scoped by file type, so they only run
when matching files are staged and are safely skipped otherwise. This makes one
config reusable across Python, Node/TS, Rust, and Go projects.

## When to use

- Adding pre-commit / git hooks to a new or existing repo
- Standardizing lint + format checks across projects
- Enforcing Conventional Commits messages
- Setting up code-quality tooling for a mixed-language codebase

## What it installs

- **General checks**: trailing whitespace, end-of-file, YAML/JSON/TOML validation,
  large-file guard, merge-conflict markers, private-key detection, line endings
- **Python**: Ruff (lint + format)
- **Node/TS/JS**: local ESLint on staged frontend files
- **Rust**: rustfmt + cargo clippy (skipped until `.rs` files exist)
- **Go**: gofmt + go vet (skipped until `.go` files exist)
- **Commit messages**: Commitizen (Conventional Commits)

## Setup steps

Run the install script from the target repository root (or pass the repo path):

```bash
bash <this-skill-dir>/scripts/install.sh [TARGET_REPO_DIR] [--yes]
```

The script will:
1. Install `pre-commit` if missing. **This is third-party software.** The script
   detects an available installer (pipx/pip/brew) and **asks for confirmation
   before installing.** Pass `--yes`/`-y` or set `PRECOMMIT_SETUP_YES=1` to skip
   the prompt (e.g. in CI). In a non-interactive session without `--yes`, it
   aborts instead of installing silently.
2. Copy `assets/pre-commit-config.yaml` → repo root as `.pre-commit-config.yaml`
   (only if one doesn't already exist).
3. Copy `scripts/eslint-staged.sh` → repo's `scripts/` (only if absent).
4. Register hooks: `pre-commit install` and `pre-commit install --hook-type commit-msg`.
5. Validate the config.

> **Third-party software notice:** Beyond the `pre-commit` tool itself, the
> first hook run will make `pre-commit` download and build isolated environments
> for the remote hooks declared in `.pre-commit-config.yaml` (e.g. Ruff,
> Commitizen). This is standard pre-commit behavior. Language toolchains used by
> local hooks (Node/ESLint, Rust rustfmt/clippy, Go gofmt/vet) are **not**
> installed for you — they must already be on the machine.

If you prefer to do it manually:
1. Copy `assets/pre-commit-config.yaml` to the repo root as `.pre-commit-config.yaml`.
2. Copy `scripts/eslint-staged.sh` to the repo's `scripts/` directory (make it executable).
3. Ensure `pre-commit` is installed (`pipx install pre-commit`).
4. Run `pre-commit install && pre-commit install --hook-type commit-msg`.

## Per-project adjustment

The ESLint hook assumes the frontend lives at `frontend/`. If your layout
differs, update BOTH:
- the `files:` path prefix in `.pre-commit-config.yaml`
  (e.g. `^web/.*\.(ts|tsx|js|jsx)$`)
- `FRONTEND_DIR` in `scripts/eslint-staged.sh`

Remove the Rust or Go blocks if those languages are never used (optional — they
auto-skip anyway).

## Verify

Run a full pass against the whole codebase:

```bash
pre-commit run --all-files
```

## Notes

- If a hook modifies files (e.g. a formatter), the commit fails; re-stage and
  commit again. This is expected.
- Bypass in emergencies with `git commit --no-verify` (avoid as a habit).
- See `references/hooks-reference.md` for a detailed explanation of every hook
  and how to add hooks for other languages (Java, Shell, etc.).
