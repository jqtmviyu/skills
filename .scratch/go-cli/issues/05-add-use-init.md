# Add, Use, and Init Commands

Status: resolved
Blocked by: 04

## User outcome

Users can discover, select, install, or use skills with the Go CLI while large Git repositories defer unrelated blob downloads until after selection.

## Acceptance criteria

- Implement `add`, `use`, and `init` with agreed aliases, core flags, noninteractive selection, and compatible prompt outcomes.
- Support repository-root discovery, explicit subpaths, `--skill`, `@skill`, multi-selection, and `--full-depth` according to existing behavior.
- Ensure repository scanning and skill selection happen before selected supporting-file materialization on filter-capable Git servers.
- Preserve `--json` schema and exit behavior for supported commands.
- Remove the snapshot download fast path; use Git or the agreed generic local/HTTP/well-known source path.
- Test commands as child processes against isolated project/home directories and local Git fixtures.

## Comments

Source of truth: `.scratch/go-cli/spec.md`.
