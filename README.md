# trysudo

Run a command with sudo when allowed, otherwise run it directly.

Non-root invocations inquire about the exact command with sudo, then replace
trysudo with sudo on success or run directly after an ordinary preflight failure.
`-n` and `--non-interactive` disable interactive sudo authentication in both
steps. Effective root runs directly without preflight. Direct execution retains
the current environment and credentials.

Interruption, a signaled preflight child, and internal errors abort without
running the target. Direct fallback emits a best-effort stderr notice. Once an
execution path is committed, failures never retry through the other path.
Real/effective UID or GID mismatches are rejected.

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

See [the v0.1 specification](docs/SPEC.md) for the runtime contract and its limitations.
