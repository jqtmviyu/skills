# Cross-Platform Release and End-to-End Acceptance

Status: ready-for-agent
Blocked by: 06

## User outcome

The independent Go CLI is validated as a cohesive tool and can be installed on the agreed operating systems and architectures.

## Acceptance criteria

- Run the CLI-level acceptance suite across add/use/remove/list/update/restore, old locks, partial-clone success/fallback, agent targets, JSON, and plugin discovery.
- Build release executables for macOS, Linux, and Windows on amd64 and arm64.
- Support the agreed `go install` distribution path.
- Document build, local test, and installation commands for the Go CLI without changing the Node CLI default.
- Run focused Go tests, race/vet/static checks selected by the project, and the existing Node regression suite where feasible.
- Complete code review against the spec and resolve findings before declaring the integration branch complete.

## Comments

Source of truth: `.scratch/go-cli/spec.md`.
