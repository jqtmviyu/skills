# Go 版 Skills CLI：兼容核心流程与按需 Git 获取

Status: ready-for-agent

## Problem Statement

当前 Skills CLI 以 Node.js 实现。用户希望有一版 Go CLI，在现有版本继续可用的同时，兼容核心命令、安装行为和 lock 文件。当前普通仓库安装通常先浅克隆当前提交的全部文件，再扫描技能并让用户选择；大型仓库会在选择前下载大量与所选技能无关的内容。更新检查的部分路径也会克隆整个当前提交。

用户不需要所有现有功能。搜索、包依赖同步、遥测、安全审计、专用 snapshot 服务和依赖特定服务器的集成可以移除。旧 Global 与 Project lock 文件必须继续读取，以便 Go 与 Node 版本可以并行使用。

## Solution

在仓库的新分支中新增独立 Go CLI 模块。第一阶段以 `skills-go` 独立命令与 Node CLI 并行，不替换现有 `bunx skills`。

保留核心安装、使用、管理和更新流程，以及现有 agent 目标、通用来源、plugin manifest 支持和 Node 版 lock 互通。移除搜索、低优先级同步、遥测/审计和专用服务端 snapshot 下载路径。

Git 获取采用 partial clone：先取提交与目录树，枚举符合现有发现规则的 `SKILL.md`，读取展示和筛选所需的最少元数据；用户选定技能后，再 materialize 对应技能目录及支持文件。更新检查也使用可用的树元数据或 partial clone，避免为检查无关仓库 blobs。

## User Stories

1. As a CLI user, I want to run the Go implementation as `skills-go`, so that I can evaluate it without changing the existing Node CLI.
2. As a CLI user, I want the core command names and aliases to remain familiar, so that I can move between the Node and Go versions with minimal retraining.
3. As a skill installer, I want to add skills from GitHub, GitLab, local paths, and standard HTTPS/SSH Git sources, so that common repositories remain usable without a vendor-specific snapshot service.
4. As a skill installer, I want to provide a repository without a skill subpath and see its available skills before downloading their full contents, so that large unrelated repository files do not delay selection.
5. As a skill installer, I want the CLI to read candidate `SKILL.md` metadata before selection, so that skill names and descriptions remain available in the selection UI.
6. As a skill installer, I want `--skill` and `@skill` selectors to resolve to repository paths before downloading supporting files, so that selecting one skill does not fetch other skills' contents.
7. As a skill installer, I want interactive multi-selection to remain available, so that I can choose one or more skills from a repository.
8. As a skill installer, I want default discovery, root-skill precedence, and `--full-depth` behavior to match the existing CLI, so that changing the fetch strategy does not silently change which skills are offered.
9. As a skill installer, I want a selected nested skill to include its scripts, references, assets, and other installable files, so that it works after installation as it did in its source repository.
10. As a skill installer, I want plugin manifests to continue contributing skill paths and grouping, so that plugin-provided skills remain discoverable and installable.
11. As a skill author, I want a repository-root `SKILL.md` to keep its current meaning as a skill whose directory is the repository root, so that existing root-level skills do not lose supporting files.
12. As a user of a Git server that supports partial clone, I want the CLI to avoid fetching blobs outside selected skill directories, so that large repositories require less network transfer and disk use.
13. As a user of a Git server that does not support partial clone, I want the CLI to fall back to a shallow clone and explain the larger download, so that installation remains usable and the performance limitation is visible.
14. As a user of a private Git repository, I want Git's configured credential helpers and SSH authentication to remain usable, so that I can install from repositories I can already access.
15. As a GitHub user, I want the CLI to use the standard GitHub Trees API for metadata-only update checks where applicable, so that a successful check does not need to download source-file contents.
16. As a project user, I want update checks to fetch only the metadata and skill contents needed to compare locked skills, so that checking an unchanged large repository does not download unrelated files.
17. As a user with a changed skill, I want update and upgrade to reinstall the locked skill path when the source supports path-based installation, so that unrelated skills are not installed or downloaded.
18. As a user whose skill has moved or been removed, I want update discovery to retain the existing name/path resolution behavior, so that the CLI can report or resolve the change instead of silently losing the lock entry.
19. As a user of the Node and Go versions in parallel, I want both versions to read and write compatible lock files, so that either version can manage the same installations.
20. As a user with an older Global lock, I want the Go version to read Global lock versions 1, 2, and 3, so that existing installations remain discoverable.
21. As a user with a Project lock, I want the Go version to read and write the existing Project lock version 1, so that project installations remain reproducible.
22. As a user with additional lock fields, I want the Go version to preserve fields it does not understand when writing a lock, so that another version or tool does not lose its data.
23. As a user of update checks, I want Global GitHub tree hashes and Project content hashes to retain their current meanings, so that Node and Go do not report false updates when alternating between versions.
24. As a user of any currently supported agent, I want its installation paths and detection rules to remain available, so that the Go CLI can install to my existing agent setup.
25. As a user, I want project/global installation, symlink/copy behavior, and supported agent-specific placement to remain compatible, so that skills land in the same locations as before.
26. As a user, I want `use` to generate the existing skill prompt and optionally launch its supported agent, so that I can use a skill without installing it.
27. As a user, I want lock-based installation recovery to continue working, so that I can restore project skills from the existing lock file.
28. As a skill author, I want `init` to create the existing `SKILL.md` template, so that I can start a skill without another tool.
29. As a script author, I want the supported JSON output fields and exit behavior to remain stable, so that automation can move to the Go binary.
30. As a user, I want prompt choices, defaults, and cancellation outcomes to remain consistent, so that the Go CLI behaves predictably even if its colors, animation, or exact wording differ.
31. As a user, I want Go release binaries for macOS, Linux, and Windows on amd64 and arm64, so that I can install the CLI without compiling it locally.
32. As a user, I want to install the Go CLI with `go install` when appropriate, so that I can use the standard Go toolchain distribution path.
33. As a user, I want the Go CLI not to send telemetry or query a remote security-audit service, so that this implementation does not depend on those service-specific integrations.

## Implementation Decisions

- Add the Go implementation as an independent module under a `go/` directory. Develop it on a new feature branch; keep the existing Node implementation intact during parallel validation.
- Name the initial executable `skills-go`. Do not change the existing `skills` or `bunx skills` entry point in this scope.
- Retain the core command surface: add, use, remove, list, update/check/upgrade, lock-based restore, and init. Retain relevant aliases and core flags. Do not implement standalone find/search or experimental package synchronization.
- Retain the current agent registry, installation destinations, global/project behavior, supported generic source formats, plugin-manifest discovery, and standardized well-known sources.
- Remove the allowlisted `skills.sh` snapshot download path, the Zapier-specific snapshot endpoint, Notion-specific flows, telemetry, security audit requests, GHES-specific API-host configuration, and `gh api` credential retrieval fallback. Use local filesystem access, standard Git HTTPS/SSH, configured Git credentials/SSH, environment tokens for standard GitHub API requests, and standard HTTP/well-known behavior where applicable.
- For Git-backed source discovery, request a depth-one partial clone without initial checkout. Enumerate tree paths, read candidate `SKILL.md` metadata and required plugin manifests, then preserve the current skill discovery and selection rules. Materialize selected skill directories and their installable supporting files after selection.
- Preserve the current meaning of root-level `SKILL.md`: the repository root is the skill directory. Installing such a skill may require all repository blobs; do not silently reduce its supporting files to only `SKILL.md`.
- If the Git server rejects or ignores object filtering, continue with a shallow-clone fallback and clearly report that this source could not save transfer. Correct installation takes priority over the bandwidth optimization.
- Preserve update semantics for Global and Project locks. Use GitHub Trees metadata for supported metadata-only checks; use partial Git fetches for project checks and API fallbacks. Fetch all files required to compute the existing hash for a locked skill directory, not unrelated repository skill payloads.
- Keep Global lock schema at version 3 and read historical Global versions 1 and 2. On a write, normalize supported older Global locks to version 3. Keep Project lock schema at version 1. Read and preserve unknown fields when updating either lock. Read-only commands must not rewrite lock files.
- Keep the existing hash contracts: Global GitHub `skillFolderHash` is the Git tree hash; Project `computedHash` is the SHA-256 content hash with current exclusions. Do not merge these into one algorithm.
- Preserve interaction semantics and JSON contracts. Exact ANSI styling, animation, and presentation copy do not need byte-for-byte compatibility.
- Build release binaries for macOS, Linux, and Windows on amd64 and arm64; support `go install` as an additional installation route.

## Testing Decisions

- Use one highest-level black-box seam: launch the Go CLI as a process with an isolated temporary HOME and project directory, backed by a local Git fixture. This seam was confirmed by the user.
- Test externally observable install and update results, lock contents, selection behavior, and whether unrelated repository blobs were transferred. Avoid making the core contract depend on Go package layout or internal function names.
- The local Git fixture must contain multiple skills, an unrelated large blob, skill support files, a plugin manifest, a root-level skill case, and configurable filter support.
- Verify that tree enumeration and metadata display happen before skill payload download; after selection, only selected skill directories and their support files are materialized when the server supports filtering.
- Verify filter rejection/ignore behavior: the CLI remains functional through shallow-clone fallback and clearly reports that filtering was unavailable.
- Exercise add and use with repository root, explicit subpath, name selector, ref, and full-depth modes. Assert that discovery results match the existing behavior.
- Exercise Global and Project update/check/upgrade flows for unchanged content, changed skill content, unrelated repository changes, moved/deleted paths, and GitHub API success/fallback. Assert stable existing hash semantics and no unrelated payload fetch on supported partial-clone paths.
- Use synthetic Global v1/v2/v3 and Project v1 lock fixtures. Verify read-only operations do not rewrite files; writes preserve unknown fields and produce versions readable by the Node implementation.
- Cover core agent destinations, global/project scope, symlink/copy mode, plugin grouping, JSON output schema/exit status, prompt selection/cancel behavior, and `init`.
- Follow prior art in the repository's CLI integration, Git fixture, source discovery, installer, lock, and update tests. Keep focused tests deterministic and avoid network access by default.

## Out of Scope

- Replacing or removing the Node CLI, changing the npm package default, or changing what `bunx skills` invokes.
- Implementing the standalone `find`/`search` feature or `experimental_sync`.
- The `skills.sh` cached snapshot API, repository allowlists for snapshots, or self-hosted snapshot endpoints.
- Notion-specific APIs, telemetry, and security-audit services.
- GitHub Enterprise-specific API host configuration or `gh api` credential fallback. Repositories remain accessible through standard Git HTTPS/SSH where credentials permit.
- Guaranteeing bandwidth savings when a Git server does not support filtering, or for root-level skills whose current payload is the entire repository.
- Byte-identical terminal colors, animations, and prompt copy.
- Changing the current Global or Project lock hash algorithms or lock semantics.

## Further Notes

- This is a substantial compatibility port, not a mechanical syntax translation. The main risk is behavioral drift across discovery, update, agent paths, and lock files; the confirmed CLI-level test seam is intended to catch that drift.
- The partial-clone design and local fixture findings are documented in `research/partial-skill-fetch-research.md`.
- The repository currently has no Go module. Implementation starts on a new branch after this spec is accepted; no branch is created as part of writing this spec.
- The repository does not have the `triage` skill installed. This local spec uses `Status: ready-for-agent` as its handoff state; no triage-label configuration is created.
