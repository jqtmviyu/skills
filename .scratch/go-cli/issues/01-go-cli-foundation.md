# Go CLI Foundation

Status: ready-for-agent

## User outcome

A separate Go executable can be built and invoked without changing the Node CLI or its `bunx skills` entry point.

## Acceptance criteria

- Add an independent Go module under the agreed Go subdirectory.
- Build an executable named `skills-go` with version/help output and command routing for the agreed core commands.
- Preserve a stable process boundary: exit codes, stderr errors, and stdout JSON mode can be exercised by an external test process.
- Add a deterministic CLI smoke test that runs the built executable.
- Keep Node sources, package behavior, and default command entry points unchanged.

## Comments

Source of truth: `.scratch/go-cli/spec.md`.
