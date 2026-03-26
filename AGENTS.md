# Repository Guidelines

## Project Structure & Module Organization
`mini-claw` is a small Go module with a single executable entrypoint in `cmd/agent/main.go`. Core runtime logic lives in `pkg/agent`, provider-related code belongs in `pkg/provider`, and local tool registration/execution is under `pkg/tools`. Keep new packages focused and place them under `pkg/<domain>` unless they are CLI-only concerns. Repository-local configuration such as `.env` stays at the root and should not be committed.

## Build, Test, and Development Commands
- `go run ./cmd/agent`: run the agent locally from the current source tree.
- `go build ./cmd/agent`: compile the CLI entrypoint and catch build-time errors.
- `go test ./...`: run all package tests. Add tests before relying on behavior changes.
- `gofmt -w ./cmd ./pkg`: format all tracked Go source files before opening a PR.

This repository does not use `pnpm` scripts today; prefer standard Go tooling.

## Coding Style & Naming Conventions
Follow idiomatic Go: tabs for indentation, `gofmt` formatting, short package names, and exported identifiers in `PascalCase`. Use `camelCase` for unexported helpers and keep files grouped by responsibility, for example `registry.go`, `file.go`, and `system.go` in `pkg/tools`. Avoid large multi-purpose files; extend the nearest domain package instead of adding cross-cutting logic to `main.go`.

## Testing Guidelines
There are currently no committed `*_test.go` files, so new features and bug fixes should add focused unit tests alongside the affected package. Name tests by behavior, for example `TestRunShell_UsesCmdOnWindows`. Prefer table-driven tests for tool executors and agent flow edge cases. Run `go test ./...` locally before requesting review.

## Commit & Pull Request Guidelines
Recent history follows Conventional Commits, including scoped messages such as `feat(agent): ...` and unscoped messages such as `feat: ...`. Continue with formats like `fix(tools): handle stderr output` or `test(agent): add tool loop coverage`. PRs should include a short summary, linked issue if applicable, test notes, and sample prompts or terminal output when changing agent behavior.

## Security & Configuration Tips
Do not commit real API keys, `.env`, or generated binaries. Load secrets from environment variables or ignored config files, and review demo prompts in `cmd/agent/main.go` before merging to avoid shipping local-only values.
