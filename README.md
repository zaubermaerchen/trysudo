# trysudo

Run a command with sudo when allowed, otherwise run it directly.

The current skeleton supports help, version, and option parsing. Command
execution is not implemented yet: a valid command reports an error on stderr
and exits with status 1.

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
