# Final Review Compatibility Fixes

Status: resolved
Blocked by: 02, 03, 04, 05, 06

## User outcome

The Go CLI preserves Node selector, legacy installation cleanup, default-branch update checking, and well-known source update behavior.

## Acceptance criteria

- `--skill` and `@skill` match the existing selector contract; a directory basename must not independently match when the skill metadata name differs.
- Global `list` and `remove` continue to detect/clean agent-specific legacy install directories when canonical/universal targets are used.
- Global GitHub Trees checks use the repository default branch when no ref is pinned, rather than selecting a stale hard-coded `main`/`master` branch.
- Global and Project update checks handle standardized well-known lock entries using the existing digest/content contract, without snapshot services or telemetry.
- Add black-box regression coverage for each case and run focused/full Go tests.

## Comments

Source of truth: `.scratch/go-cli/spec.md` lines 26, 45, 61, 65; findings from final Spec review.
