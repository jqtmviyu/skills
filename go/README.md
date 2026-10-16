# Skills Go CLI

The Go CLI manages Agent Skills and is released as a standalone `skills` executable. Download the asset for your operating system and architecture from the [GitHub Releases](https://github.com/jqtmviyu/skills/releases) page.

The Node.js implementation and its npm distribution remain unchanged. This Go binary is distributed through GitHub Releases, not npm.

## Download a release asset

Release assets use these names:

- `skills-darwin-amd64`, `skills-darwin-arm64`
- `skills-linux-amd64`, `skills-linux-arm64`
- `skills-windows-amd64.exe`, `skills-windows-arm64.exe`

After downloading the matching asset, make it executable on macOS/Linux and place it in a directory on your `PATH`:

```sh
chmod +x skills-darwin-arm64
mkdir -p "$HOME/.local/bin"
mv skills-darwin-arm64 "$HOME/.local/bin/skills"
```

Replace the asset name with the one matching your OS and CPU architecture. On Windows, keep the `.exe` suffix.

## Build from a checkout

From the repository root:

```sh
cd go
go test ./... -count=1
go vet ./...
go build -o ./skills ./cmd/skills
./skills --help
```

## Commands

The Go CLI provides `add`, `use`, `init`, `list`, `remove`, `update` (`check` / `upgrade`), and lock-based restore. For available options, run:

```sh
skills --help
skills add --help
skills update --help
```

See [COMPATIBILITY.md](./COMPATIBILITY.md) for the CLI, JSON, configuration, and lock-file contract.

## Release workflow

When a versioned GitHub Release is published in this repository, GitHub Actions builds and uploads the six platform assets above. The existing publishing workflow dispatches the asset workflow after creating a Release; Releases created manually trigger it directly. Both paths upload to the repository that owns the Release and use GitHub's built-in `GITHUB_TOKEN`.
