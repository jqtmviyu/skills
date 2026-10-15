# List, Remove, Restore, and Update Commands

Status: claimed
Blocked by: 03, 05

## User outcome

Users can manage installed skills and check or apply updates without downloading unrelated repository blobs where partial fetch is supported.

## Acceptance criteria

- Implement `list`, `remove`, lock-based restore, and `update`/`check`/`upgrade` with the agreed aliases and core flags.
- Preserve Global GitHub Trees metadata checks where available; use partial Git scans for Project checks and API fallback.
- Compare existing lock hashes using their existing algorithms and resolve moved/deleted skills compatibly.
- Reinstall changed skills using the locked skill path when supported; preserve generic Git fallback behavior otherwise.
- Verify unchanged sources, selected-skill changes, unrelated repository changes, deleted/moved skills, legacy locks, and unsupported filter fallback using end-to-end fixtures.

## Comments

Source of truth: `.scratch/go-cli/spec.md`.
