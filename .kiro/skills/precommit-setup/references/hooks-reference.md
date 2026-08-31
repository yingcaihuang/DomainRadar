# Pre-commit Hooks Reference

This document explains each hook in the bundled `pre-commit-config.yaml` template,
how the language scoping works, and how to adapt it per project.

## How language scoping works

Every language-specific hook uses a `files:` regex so it only runs when matching
files are staged. When no matching files exist, pre-commit reports the hook as
`Skipped (no files to check)` — it never errors. This is what makes the config
safe to drop into any repo regardless of the languages it uses.

Rust, Go, and Node hooks use `language: system`, meaning they call the tool
already installed on your machine. They require those toolchains to be present:
- Node/TS/JS → `node` + `npm`/`npx`, plus an ESLint config in the frontend dir
- Rust → `rustfmt`, `cargo clippy`
- Go → `gofmt`, `go`

## General file checks (all languages)

| Hook | Purpose |
|------|---------|
| `trailing-whitespace` | Strips trailing whitespace at end of lines |
| `end-of-file-fixer` | Ensures every file ends with exactly one newline |
| `check-yaml` | Validates YAML syntax (multi-document allowed) |
| `check-json` | Validates JSON syntax (tsconfig.*.json excluded — they allow comments) |
| `check-toml` | Validates TOML syntax (Cargo.toml, pyproject.toml, etc.) |
| `check-added-large-files` | Blocks files larger than 500 KB |
| `check-merge-conflict` | Detects leftover merge conflict markers |
| `detect-private-key` | Prevents committing private keys |
| `mixed-line-ending` | Normalizes line endings to LF |

## Python — Ruff

| Hook | Purpose |
|------|---------|
| `ruff` (`--fix`) | Lints Python and auto-fixes what it can |
| `ruff-format` | Formats Python code (Black-compatible) |

Scoped to `\.py$`. Ruff is downloaded automatically by pre-commit — no local
install needed.

## Node / TypeScript / JavaScript — ESLint

Runs the project's local ESLint (via `scripts/eslint-staged.sh`) on staged
frontend files only. It uses your repo's own `eslint.config.*` and dependencies,
so behavior matches `npm run lint`.

**Per-project adjustment (important):**
- Default path prefix assumes frontend lives at `frontend/`.
- If yours is elsewhere (e.g. `web/`, `apps/ui/`, `azure-voice-admin/frontend/`):
  1. Edit the `files:` regex in `.pre-commit-config.yaml`
     (e.g. `^web/.*\.(ts|tsx|js|jsx)$`).
  2. Edit `FRONTEND_DIR` in `scripts/eslint-staged.sh` to the same path.

## Rust — rustfmt + clippy

| Hook | Purpose |
|------|---------|
| `rustfmt` | Formats Rust code (edition 2021) |
| `cargo-clippy` | Lints with clippy, treats warnings as errors (`-D warnings`) |

Scoped to `\.rs$`. Remove this block if you never use Rust.

## Go — gofmt + go vet

| Hook | Purpose |
|------|---------|
| `gofmt` | Formats Go code in place |
| `go-vet` | Runs `go vet ./...` static analysis |

Scoped to `\.go$`. Remove this block if you never use Go.

## Commit message linting — Commitizen

Enforces [Conventional Commits](https://www.conventionalcommits.org)
(`feat:`, `fix:`, `docs:`, `chore:`, etc.) on the commit message. Runs at the
`commit-msg` stage, which requires `pre-commit install --hook-type commit-msg`.

## Adding hooks for other languages

Follow the same pattern — a `local` repo block with `language: system` and a
`files:` regex scoping it to that language's extension. Examples:

```yaml
  # Java — google-java-format
  - repo: local
    hooks:
      - id: google-java-format
        name: google-java-format
        entry: google-java-format --replace
        language: system
        files: \.java$

  # Shell — shellcheck
  - repo: https://github.com/shellcheck-py/shellcheck-py
    rev: v0.10.0.1
    hooks:
      - id: shellcheck
        files: \.sh$
```

## Common issues

- **"files were modified by this hook" then commit fails**: a formatter changed
  files. Re-stage (`git add`) and commit again. This is expected behavior.
- **Hook skipped unexpectedly**: check the `files:` regex matches your paths.
- **`system` hook fails with "command not found"**: install the toolchain, or
  remove that hook block if the language isn't used.
- **Bypass in an emergency**: `git commit --no-verify` (avoid as a habit).
