# Contributing to aws_explorer

Thanks for helping improve `aws_explorer` — a read-only Go CLI + TUI for
exploring, auditing, and cost-analyzing AWS accounts.

## Core principles

These are non-negotiable (see [`CLAUDE.md`](CLAUDE.md) for the full rationale):

1. **Deterministic, no AI.** Analyses are pure functions over data AWS returns,
   unit-tested with fixtures.
2. **Read-only by default.** Anything that mutates AWS, leaves the AWS API, or
   incurs a charge is opt-in behind an explicit, cost-stating confirmation.
3. **Best-effort collection.** A denied/failed API call degrades *that one*
   feature with a visible note — it must never crash or abort the whole run.
   Distinguish "not set" from "denied" from "failed" (never swallow an error
   into a default).
4. **One UX language.** Reuse the shared `internal/ui` / `internal/table`
   theme/table/scrollbar widgets; register new TUI keys in `internal/ui`.

## Development workflow

Requires Go (see the `go` directive in [`go.mod`](go.mod)).

```bash
make fmt vet test    # gofmt, go vet, full go test ./... — run before every commit
make build           # builds ./bin/aws_explorer with version ldflags
```

CI mirrors this and additionally runs `go test -race`, cross-compiles for
linux/darwin/windows × amd64/arm64, and runs `govulncheck`. `golangci-lint` runs
in advisory mode. Keep `gofmt -l` clean — a single misindented line fails CI.

## Branches & pull requests

- The default branch is **`master`**; target PRs at `master` directly (don't
  stack PRs onto an unmerged feature branch).
- Keep changes focused; add or update tests with every behavior change.
- Update the relevant docs (`README.md`, `docs/`) when behavior changes.
- After opening a PR, new commits need to land before merge — they don't ride
  along into an already-merged PR.

## Architecture & adding a collector

Start with [`docs/architecture.md`](docs/architecture.md). Briefly:

- Commands live in `cmd/`; everything else under `internal/` — service
  collectors in `internal/services/*`, the scan engine in `internal/engine`,
  shared TUI widgets in `internal/ui` + `internal/table`, pure analysis in
  `internal/findings` / `internal/vpctui`.
- A new service collector is a typed `List*`/`Describe*` implementation
  registered with the engine (don't rely on the tag sweep alone). Guard every
  SDK pointer deref (`aws.ToString` / `aws.ToInt32` / …), paginate all `List*`
  calls, and stamp `Region` on each resource.

## Security

Never commit credentials. See [`SECURITY.md`](SECURITY.md) for how to report
vulnerabilities and how to handle AWS credentials safely (prefer SSO / profiles
/ STS over long-lived keys).
