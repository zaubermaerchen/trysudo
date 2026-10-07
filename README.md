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
Real/effective UID or GID mismatches are rejected. Ordinary sudo absence can
fall back to direct execution; unexpected sudo lookup or runtime errors, such
as a symlink loop (`ELOOP`), abort without running the target.

Go runtime initialization and the final exec handoff do not guarantee perfect
preservation of signal dispositions. Signals ignored before trysudo starts,
notably `SIGTERM`, `SIGQUIT`, or `SIGPIPE`, may not remain ignored in the final
target process, depending on the runtime and launch environment. This does not
change the rule that an interruption observed by trysudo prevents target execution.

Standard I/O inheritance refers to the descriptors received after Go runtime
initialization. If fd 0, 1, or 2 was closed before launch, the runtime may reopen
it, for example to `/dev/null`; its original closed state is not guaranteed to
reach the target.

Go 1.26 or newer is required.

```sh
go build -o trysudo .
./trysudo --help
./trysudo --version
go test ./...
```

Development builds report `trysudo devel`. Set a version at build time with
`go build -ldflags '-X main.version=v0.1.0' -o trysudo .`.

Real execution has been verified on Linux with upstream sudo 1.9.x. macOS
and sudo-rs remain best effort until separately verified. CI checks Ubuntu and
macOS with Go 1.26 and 1.27, but passing its test suite does not establish
real-sudo compatibility on macOS. The manually triggered
**Build Unix binaries** workflow uploads Linux and macOS binaries for amd64
and arm64 as workflow artifacts. Extract the included `trysudo.tar.gz` to
preserve the binary's executable permissions. It does not publish GitHub Releases.

See [the v0.1 specification](docs/SPEC.md) for the runtime contract and its limitations.
