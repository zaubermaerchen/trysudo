trysudo v0.1.0 performs a command-specific sudo preflight, then executes the target through the selected sudo or direct path. Use `-n` for noninteractive sudo checks.

- Runs the target at most once and never retries directly after committing to sudo.
- Falls back to direct execution for ordinary sudo unavailability or preflight denial, with a best-effort stderr notice.
- Observed preflight interruptions prevent target execution and return `128 + signal number`; unexpected internal/lookup errors abort.

Download the Linux/macOS amd64/arm64 archives below and verify them against `SHA256SUMS`. Archives preserve executable permissions.

Known limitations: Go runtime initialization may change inherited signal dispositions and reopen closed standard descriptors. Preflight is advisory and cannot prevent policy or filesystem changes before final execution. Real-sudo verification is limited to one manual session on Ubuntu Linux amd64 with Ubuntu-packaged upstream sudo 1.9.15p5, using a Go 1.24.7 build after temporarily lowering the working-copy go.mod directive. It does not validate the official Go 1.26 build; macOS real-sudo compatibility and sudo-rs remain unverified/best effort. See README and docs/SPEC.md for the precise contract.
