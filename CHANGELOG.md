# Changelog

All notable changes to this project are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.0.0] — unreleased

First tagged release. From here the exported API is a compatibility promise:
it will not change incompatibly before 2.0.0.

preflight was extracted from the startup self-checks of a GitHub Actions runner
supervisor, where the failures it reports on — a directory the process cannot
write to, a network that no longer exists, a docker socket the user has no
permission on — were being discovered by the first job that needed them rather
than at startup.

**Requires Go 1.27 or newer, and Unix.** The kits track the current Go release
together. Note that a library's `go` directive is a hard minimum for everyone
who imports it: `go get` raises the consumer's own `go.mod` to match.
`FileGID` reads POSIX ownership through `syscall.Stat_t`, so the package does
not build on Windows.

### What 1.0.0 provides

- The harness: `Check`, `NamedCheck`, `CheckFunc`, `Named`, `Run` and
  `Results`, with `OK`, `Warn` and `Fail` building the verdicts. Checks run in
  the order given, sequentially, and a panicking check becomes an error result
  rather than taking the program down.
- A `Result` that carries a `Hint` as well as a `Message`, because a check
  reporting a problem without saying what to do about it has moved the work
  rather than done it.
- Built-in probes: `DirWritable`, `PathExists`, `TCPReachable`,
  `SocketAccessible`, `SubdirsPrivate` and `MissingCommands`.
- `Results.Log`, `Results.Problems`, `Results.Worst`, `Results.OK` and
  `Results.SortedByLevel` for reporting — log output in check order, worst-first
  for a UI.
- The diagnosis half: `Classifier`, `Diagnosis`, `Advice`, `Rule`, `Wrap` and
  `KindOf`, which answer the same question ("what should I do about this?") for
  failures that happen after startup. `Advice` keeps `CheckCommand` and
  `FixCommand` separate so the read-only one can always be offered first.

### Settled before the release

These are API decisions rather than changes anyone can observe, since nothing
was tagged before. They are recorded because 1.0.0 is where they stop being
adjustable.

- **`NamedCheck` is now an exported interface.** `Run` names a panicking or
  timed-out check by asking the `Check` for its name — neither Result comes
  from the check itself, so there is nothing else to take a name from. That
  contract was discovered through an anonymous `interface{ Name() string }`
  assertion and documented nowhere, so a `Check` with a type of its own had no
  way to know it could opt in. `Named` now returns `NamedCheck`.
- **`CommandsPresent` is now `MissingCommands`.** It never checked anything —
  the caller does the lookup and passes the missing list, deliberately, because
  where the commands have to be (this host, an image, another machine) decides
  how to ask. The name now says what it takes rather than implying a probe.
- **`Dir` is gone.** It was `filepath.Join` under another name, and a frozen
  API is the wrong place to keep an alias.

### Also in the repository

- `SECURITY.md`, covering the two things a caller has to get right: results
  name real paths, uids and addresses on purpose, so they are a log rather than
  a status page; and hints are commands this package never runs, which a caller
  should not run automatically either.
- Runnable examples in `example_test.go` that `go test` verifies, as an
  external test package, so they compile only against the exported API and
  cannot drift from it.
- CI covering formatting, vet, tests, golangci-lint and govulncheck, with the
  test job run on Linux and macOS. Every job takes its Go version from
  `go.mod`. The HTML coverage report is uploaded as a build artifact; no
  coverage service is involved.
- A Go Report Card workflow, run on demand, that regenerates the badge and
  report and commits them back.

[1.0.0]: https://github.com/soulteary/preflight-kit/releases/tag/v1.0.0
