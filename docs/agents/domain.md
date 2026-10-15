# Domain Docs

How engineering skills should consume this repo's domain documentation.

## Before exploring, read these

- `GLOSSARY.md` at the repo root, or `GLOSSARY-MAP.md` if it exists; for a map, read the glossary relevant to the topic.
- `docs/adr/` for decisions relevant to the area. In multi-context repos, also check context-scoped ADRs.

If these files do not exist, proceed without treating their absence as a problem. Create them when domain terms or decisions need to be recorded.

## File structure

This is a single-context repo:

- `GLOSSARY.md` at the repo root
- ADRs under `docs/adr/`

## Use the glossary's vocabulary

Use established domain terms in issues, proposals, and tests. If a needed term is missing, reconsider the terminology or record the gap for domain modeling.

## Flag ADR conflicts

Surface any contradiction with an existing ADR instead of silently overriding it.
