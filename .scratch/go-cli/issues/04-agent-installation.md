# Agent Registry and Skill Installation

Status: ready-for-agent
Blocked by: 02, 03

## User outcome

The Go CLI installs and removes skills in the same agent directories and scopes as the existing CLI.

## Acceptance criteria

- Port the currently supported agent definitions, detection rules, project/global destinations, environment overrides, and Eve subagent placement.
- Preserve symlink/copy behavior, path safety, and installer exclusions.
- Support plugin-manifest skill paths and grouping.
- Keep installation results and errors observable through the CLI process.
- Verify representative and registry-wide agent destinations against the current compatibility data.

## Comments

Source of truth: `.scratch/go-cli/spec.md`.
