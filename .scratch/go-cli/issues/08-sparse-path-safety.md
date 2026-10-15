# Sparse Checkout Path Safety

Status: ready-for-agent
Blocked by: 02

## User outcome

A repository cannot use a newline-containing skill path to expand sparse-checkout patterns and materialize blobs outside the selected skill directory.

## Acceptance criteria

- Add a public behavior regression test through the scanner materialization API using a local Git fixture with a selected path containing a newline.
- Either encode the path safely as one sparse-checkout rule or reject it with a clear error before fetching/materializing unrelated paths.
- Verify the fixture's unselected large blob remains absent when filtering is supported.
- Preserve current source parsing behavior for ordinary refs and paths; do not add slash-branch parsing that differs from the Node compatibility contract.

## Comments

Source of truth: `.scratch/go-cli/spec.md`, ticket 02, and the read-only scanner review.
