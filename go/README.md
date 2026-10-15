# skills-go

`skills-go` is the Go CLI for managing Agent Skills. It is distributed separately from the existing Node.js `skills` CLI and does not change the `bunx skills` or npm entry points.

## Install

Install the latest Go module release:

```sh
go install github.com/vercel-labs/skills/go/cmd/skills-go@latest
```

The Go module lives in the repository's `go/` directory. Release tags use the `go/vX.Y.Z` prefix required for a module rooted in a subdirectory. The installed command is named `skills-go` and should be available on the Go bin directory in your `PATH`.

Prebuilt binaries for macOS, Linux, and Windows (`amd64` and `arm64`) are attached to the matching GitHub Release. Download the asset for your operating system and architecture, then place the executable on your `PATH`.

## Build from a checkout

From the repository root:

```sh
cd go
go test ./...
go vet ./...
go build -o ./skills-go ./cmd/skills-go
./skills-go --help
```

To install the checkout into a temporary bin directory:

```sh
cd go
GOBIN="$(mktemp -d)" go install ./cmd/skills-go
```

## Commands

The Go CLI provides `add`, `use`, `init`, `list`, `remove`, `update` (`check` / `upgrade`), and lock-based restore. For command options, run:

```sh
skills-go --help
skills-go add --help
skills-go update --help
```
