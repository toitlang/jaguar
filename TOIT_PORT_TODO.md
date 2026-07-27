# Completing the Toit Jaguar port

This branch is a working checkpoint, not the point at which the Go host should
be removed. Keep the Go implementation available as the parity reference until
the items below are implemented and their end-to-end tests pass.

`TOIT_SDK_GAPS.md` records the upstream facilities that were missing while
building this checkpoint. Once one of those facilities lands, update the
pinned SDK or package first, then finish the matching Jaguar work and tests
listed here.

## Command parity

- [ ] Finish `jag setup`.
  - Add `certificate-roots` after package locking is recoverable.
  - Download over HTTPS with `http.Client` and the system trust roots.
  - Decode the response with `zlib.GzipDecoder`, extract it with `tar`, and
    atomically install the SDK and assets.
  - Preserve `--check`, `--print-path`, `--sdk-version`, and cache cleanup.
  - Test against a local TLS server and a small tar.gz fixture; do not depend
    on the release service in the test.
- [ ] Add SDK command pass-through.
  - Extend `pkg-cli` with a command mode that stops parsing at the subcommand.
  - Implement `jag toit ...`, `jag pkg ...`, and the hidden `jag toit lsp`
    compatibility path.
  - Pass arguments byte-for-byte and propagate the child exit status.
  - Add completion and command tests containing unknown short and long
    options, `--`, and option values that begin with `-`.
- [ ] Implement `jag watch`.
  - Add host filesystem notifications and child-process termination to the
    SDK.
  - Use the SDK dependency file to maintain the watched file set.
  - Cancel an obsolete compile or run before starting its replacement.
  - Test source writes, atomic renames, dependency additions/removals, rapid
    consecutive changes, compile failures, and Ctrl-C cleanup.
- [ ] Finish serial-port selection.
  - Add portable serial enumeration and USB metadata to the host UART API.
  - Implement `jag port --list` in short, JSON, and YAML formats.
  - Implement validated and interactive `jag port set`.
  - Remove the temporary need for `--skip-port-check`.
  - Test no ports, one port, multiple ports, stale configured ports, and
    permission errors.
- [ ] Finish native backtrace decoding.
  - Add a supported SDK API that accepts a backtrace and envelope.
  - Use it in both `jag decode` and streamed `jag monitor` output.
  - Test known backtrace/envelope fixtures and malformed input.
- [ ] Restore `jag run -s`.
  - Expose immediate expressions through the SDK runner.
  - Test expressions, thrown exceptions, arguments, and exit status.
- [ ] Add portable no-echo terminal input.
  - Use it for interactive WiFi password entry.
  - Test with a pseudo-terminal and verify that the password is absent from
    captured output.
- [ ] Finish UART proxying and monitor shutdown after process termination is
  available. Test disconnect/reconnect, Ctrl-C, and simultaneous log traffic.
- [ ] Compare setup update checks and analytics with the Go reference and
  either port them or explicitly retire them.

## SDK and package follow-ups

- [ ] Fix `toit info sdk` so `lib-path` points at the actual SDK library
  directory, and add a test that resolves a known SDK library through it.
- [ ] Fix `toit pkg search --output-format=json`.
- [ ] Define package-lock recovery semantics.
  - Include the lock path and owner/process information in errors.
  - Provide a safe recovery operation for an abandoned project lock.
  - Cover normal release, killed owner, updater failure, and contending
    processes in lockfile/package-manager tests.
- [ ] Decide how a Debian source package declares the pinned Toit compiler as
  a build dependency, then remove the CI-only preinstallation assumption.

## Discovery and compatibility

- [ ] Decide whether mDNS becomes the default discovery transport.
  - During migration, keep UDP broadcast discovery enabled so existing Jaguar
    installations can still find devices.
  - Test duplicate replies, address changes, multiple interfaces, IPv4/IPv6,
    and devices advertising through only one transport.
- [ ] Compare every command, flag, configuration key, output format, and exit
  status against the Go CLI. Record intentional incompatibilities in
  `README.md`.

## Release gate

- [ ] Run unit, completion, host integration, and package tests on Linux,
  macOS, and Windows.
- [ ] Run the WiFi suite on both ESP32 and ESP32-S3 with
  `github.com/toitlang/qemu`.
- [ ] Run the hardware suite on `/dev/ttyUSB4`, then on ESP32-S2, S3, C3, and
  C6 as those boards become available.
- [ ] Recheck Linux ARM and ARM64 cross-compilation and all Debian packaging.
- [ ] Exercise install, upgrade, downgrade, and rollback from a published
  release artifact.
- [ ] Remove the Go sources, `go.mod`, and `go.sum` only after the parity
  matrix and release gate are green.
