# SDK gaps found by the Toit Jaguar host

The Jaguar host intentionally does not emulate missing SDK facilities with
polling, platform-specific subprocesses, or private primitives. The following
items need SDK or package support before the corresponding Jaguar commands can
reach parity with the Go implementation.

## Blocking command parity

- Process cancellation: `host.pipe.Process` has no public terminate, kill, or
  signal operation. `jag watch`, graceful monitor shutdown, and UART proxy
  cleanup need a portable way to stop a child process.
- Filesystem notifications: there is no host filesystem watch API. `jag watch`
  needs write/rename notification for a changing dependency set.
- CLI pass-through: `pkg-cli` rejects unknown options and has no command mode
  that stops option parsing. `jag toit ...` and `jag pkg ...` need to pass
  arbitrary arguments, including options, to the SDK.
- Serial enumeration: the host UART API can open a known path but cannot list
  serial ports or expose USB metadata. `jag port --list` and interactive
  `jag port set` need this.
- Native stack traces: the SDK CLI can decode Toit system messages, but there
  is no supported Toit API for decoding native backtraces with an envelope.
  This affects `jag decode` and `jag monitor`.
- Immediate expressions: the Go Jaguar accepted `jag run -s <expression>`,
  but SDK `v2.0.0-alpha.196` does not expose the corresponding `toit run -s`
  operation.
- Password input: the host terminal API has no portable no-echo input. This is
  needed for interactive WiFi password entry.

## Package-manager failures encountered

- `toit info sdk --output-format=json` reports `lib-path` as
  `<sdk-dir>/lib`, but SDK libraries are installed in
  `<sdk-dir>/lib/toit/lib`. The existing SDK test only checks that the
  `lib-path` field is present, not that it resolves to the SDK library
  directory.
- `toit pkg search cli --output-format=json` terminates with
  `INVALID_JSON_OBJECT`.
- After a manually changed `package.yaml`, `toit pkg install --recompute`
  rejects the stale lock instead of recomputing it.
- An interrupted package operation can leave `.packages/.lock` behind. Once
  its modification time has stopped changing for roughly 1.5 seconds, the
  next operation throws a bare `LOCK_STALE`. The error does not identify the
  lock, say which process owned it, or offer a supported recovery operation.
  `tools/repro-pkg-lock.sh` contains a minimal reproduction. The abandoned
  lock currently prevents adding the `certificate-roots` package, which
  blocks an HTTPS implementation of `jag setup`.

`PERMISSION_DENIED` was also observed while `toit pkg list` acquired the
global registry cache lock. It is not currently classified as an SDK defect:
the command was running in a workspace sandbox that allowed reading, but not
updating, the cache under `$HOME/.cache/toit`. The same command succeeds
outside that sandbox. The stack trace nevertheless makes this hard to
diagnose because it reports the failed `mkdir` without the path.

## Distribution

Debian source builds need a supported way to declare/install the pinned Toit
compiler as a build dependency. CI installs it before invoking
`dpkg-buildpackage`, but `debian/control` cannot currently express that
requirement using a Debian package.
