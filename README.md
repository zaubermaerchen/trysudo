# trysudo

Run a command with sudo when allowed, otherwise run it directly.

Commands currently run directly with the current environment and credentials,
replacing trysudo via Unix exec. sudo discovery and preflight are implemented
but are not yet connected to the CLI; `-n` and `--non-interactive` are accepted
but have no runtime effect.

Go 1.26 or newer is required.

```sh
go build -o trysudo .
./trysudo --help
./trysudo --version
go test ./...
```

Development builds report `trysudo devel`. Set a version at build time with
`go build -ldflags '-X main.version=v0.1.0' -o trysudo .`.

CI checks Ubuntu and macOS with Go 1.26 and 1.27. The manually triggered
**Build Unix binaries** workflow uploads Linux and macOS binaries for amd64
and arm64 as workflow artifacts. Extract the included `trysudo.tar.gz` to
preserve the binary's executable permissions. It does not publish GitHub Releases.

See [the v0.1 specification](docs/SPEC.md) for the planned runtime behavior.
