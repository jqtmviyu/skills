# Git Skill Scanner with Partial Fetch

Status: claimed
Blocked by: 01

## User outcome

A Git repository can be scanned for skills before full skill contents are downloaded, and selected skill directories can be materialized on demand.

## Acceptance criteria

- Support local, GitHub, GitLab, and generic HTTPS/SSH Git sources through the system Git client.
- For supported servers, fetch a shallow partial repository without initial checkout; enumerate tree paths and read only candidate skill metadata and required plugin manifests before selection.
- Preserve current default discovery precedence/depth, `--full-depth`, subpath, ref, and root-level `SKILL.md` behavior.
- Materialize selected skill directories with their installable support files after selection.
- If filter negotiation is rejected or ignored, continue through shallow-clone fallback and report that transfer filtering was unavailable.
- Provide local Git fixture coverage for filter-capable and filter-incapable servers, metadata reads, selected files, unrelated large blobs, and cleanup.

## Comments

Source of truth: `.scratch/go-cli/spec.md` and `research/partial-skill-fetch-research.md`.
