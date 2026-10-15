# Lock Compatibility and Hash Contracts

Status: resolved
Blocked by: 01

## User outcome

The Go CLI can safely share Global and Project lock files with the existing Node CLI, including historical Global lock versions.

## Acceptance criteria

- Read Global lock versions 1, 2, and 3; write version 3 when a mutation occurs.
- Read and write Project lock version 1.
- Preserve unknown top-level and entry fields when writing supported lock versions.
- Do not rewrite lock files during read-only commands.
- Preserve Global GitHub tree-hash and Project content-SHA-256 semantics and current exclusions.
- Cover legacy, malformed, missing, relative local source, and unknown-field fixtures; verify resulting files remain readable by the Node contract.

## Comments

Source of truth: `.scratch/go-cli/spec.md`.
