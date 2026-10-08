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

Install with Homebrew on Linux or macOS:

```sh
brew install zaubermaerchen/tap/trysudo
trysudo --version
trysudo --help
```

Alternatively, download the archive for your operating system (`linux` or
`darwin`) and architecture (`amd64` or `arm64`) together with `SHA256SUMS`
from [GitHub Releases](https://github.com/zaubermaerchen/trysudo/releases).
For example, to install v0.1.0 on Linux amd64, run these commands in a directory
containing only the downloaded archive and checksum file:

```sh
sha256sum --ignore-missing --check SHA256SUMS &&
  tar -xzf trysudo-v0.1.0-linux-amd64.tar.gz &&
  mkdir -p "$HOME/.local/bin" &&
  install -m 755 trysudo "$HOME/.local/bin/trysudo" &&
  "$HOME/.local/bin/trysudo" --version &&
  "$HOME/.local/bin/trysudo" --help
```

Continue only if the checksum check succeeds. On macOS, use
`shasum -a 256 --ignore-missing --check SHA256SUMS` and the matching `darwin`
archive. Add `$HOME/.local/bin` to your `PATH` if it is not already present.

Building from source requires Go 1.26 or newer.

```sh
go build -o trysudo .
./trysudo --help
./trysudo --version
go test ./...
```

Development builds report `trysudo devel`. Set a version at build time with
`go build -ldflags '-X main.version=v0.1.0' -o trysudo .`.

One manual real-sudo verification session covered Ubuntu Linux amd64 with
Ubuntu-packaged upstream sudo 1.9.15p5. The binary was built with Go 1.24.7 after
temporarily lowering the `go.mod` directive in a working copy; this does not
validate the project's official Go 1.26 build. macOS and sudo-rs are unverified
and remain best effort.

CI checks Ubuntu and macOS with Go 1.26 and 1.27, but passing its Go test suite
does not establish real-sudo execution compatibility. The **Build Unix binaries**
workflow builds Linux and macOS archives for amd64 and arm64, checks their
contents and checksums, and smoke-tests generated binaries on Linux and macOS.
Pushing a stable `vMAJOR.MINOR.PATCH` tag publishes the complete artifact set
and `SHA256SUMS` to
GitHub Releases only after those checks pass. Manual workflow runs build and
verify artifacts without publishing a release.

See [the v0.1 specification](docs/SPEC.md) for the runtime contract and its limitations.
