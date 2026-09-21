# preflight-kit

[![CI](https://github.com/soulteary/preflight-kit/actions/workflows/ci.yml/badge.svg)](https://github.com/soulteary/preflight-kit/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/soulteary/preflight-kit.svg)](https://pkg.go.dev/github.com/soulteary/preflight-kit)
[![Go Report Card](.github/goreportcard.svg)](.github/goreportcard-report.md)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)

Startup self-checks that say what to do, not just what is wrong. Zero dependencies.

**Docs:** English · [中文](README_CN.md)

## The problem is timing

Most deployment mistakes — a directory the process cannot write to, a network that no longer exists, a socket the user has no permission on — are perfectly detectable the moment the process starts.

They are instead discovered much later, by the first piece of real work that needs them. By then the failure surfaces as whatever *that* work happens to fail with, at a moment nobody is watching, in a message that says nothing about the setting that caused it.

```go
results := preflight.Run(ctx,
    preflight.Named("data directory", func(ctx context.Context) preflight.Result {
        return preflight.DirWritable("data directory", cfg.DataDir)
    }),
    preflight.Named("docker socket", func(ctx context.Context) preflight.Result {
        return preflight.SocketAccessible("docker socket", "/var/run/docker.sock")
    }),
)
results.Log(log.Printf)
```

```
[preflight ok] data directory: /srv/data is writable (uid 1001)
[preflight !]  docker socket: /var/run/docker.sock is owned by gid 998 and this process is not
               in that group; access will be denied
               -> add the process to gid 998 (docker-compose: group_add: ["998"])
```

## Install

```bash
go get github.com/soulteary/preflight-kit
```

The package is named `preflight`, not `preflight-kit`, so spell the import name out:

```go
import preflight "github.com/soulteary/preflight-kit"
```

## A finding without a fix has moved the work, not done it

`Result` carries a `Hint` as well as a `Message`, and the message is expected to name the actual path, address or value involved.

> `/srv/runners is not writable by uid 1001` sends the reader somewhere.
> `permission problem` does not.

## Three rules the package holds to

**Checks are read-only.** Preflight runs on every start, including starts that are already going badly. A self-check with side effects is one more thing that can make a bad situation worse.

**A failing check never stops the program.** Reporting is the whole job. A startup self-check that refuses to start is a self-check that will be removed the first time it's wrong about an environment its author didn't anticipate.

**Passing results are logged too.** `docker daemon 27.0.3, network app-net exists, /srv/data writable by uid 1001` is the record of what the environment looked like — and it's what someone reads first when the same deployment misbehaves three weeks later.

## Built-in probes

| Probe | What it actually checks |
|---|---|
| `DirWritable` | Create **and remove** a file. See below |
| `SocketAccessible` | The socket exists *and* this process is in its group |
| `TCPReachable` | Something answers — always bounded by a timeout |
| `SubdirsPrivate` | Which directories let other users in. Reports; never changes |
| `PathExists`, `CommandsPresent`, `FileGID`, `InGroup` | |

### Why `DirWritable` also deletes

A directory can accept a new file and refuse to let you delete it — a sticky bit, or a mount that turns read-only under the first write. A check that stops at "created it" calls that directory fine. The program then fails later, on the first thing that needs to replace or clean up a file, with no connection back to this setting.

The probe file has a random name, because a fixed one collides with a real file of that name — and a self-check that opens and deletes the user's data is worse than no self-check at all.

### Why `TCPReachable` is always bounded

An unreachable address that blackholes packets hangs until the TCP stack gives up, which is far longer than anyone will wait at startup for a check meant to be reassuring. A zero timeout becomes `DefaultDialTimeout`, not "forever".

### Why `SubdirsPrivate` only reports

Tightening permissions can break a deployment whose uids don't line up the way the checker assumes. The decision belongs to whoever can see the whole picture; the check's job is to make sure they know.

## Diagnosing failures that happen later

Preflight answers *"is this deployment sound"* at startup. `Classifier` answers the other half: when something fails at runtime, what kind of failure is it and what should be done about it.

```go
var classifier = preflight.Classifier{
    Rules: []preflight.Rule{{
        Kind:       "docker-access",
        Substrings: []string{"permission denied", "cannot connect to the docker daemon"},
        Advice: preflight.Advice{
            Suggestion:   "check the socket mount and group membership",
            CheckCommand: "ls -l /var/run/docker.sock && id",
            FixCommand:   "usermod -aG docker $USER && systemctl restart docker",
        },
    }},
    Fallback: preflight.Advice{CheckCommand: "docker compose ps"},
}

d := classifier.Diagnose(err)   // Kind, Error, Suggestion, CheckCommand, FixCommand
```

**`CheckCommand` and `FixCommand` are separate on purpose.** Someone diagnosing a problem on a machine they don't own needs to look before they touch, and a single "run this" field forces the author to choose between giving a *safe* command and giving a *useful* one. Splitting them means the read-only one can always be offered first — and a UI can present the other as an action rather than as information.

**Tag errors where they happen; match text only as a fallback.**

```go
return preflight.Wrap("agent-connect", err)
```

An error tagged at the point of failure knows what it is. Matching on message text is guesswork that goes wrong *quietly* — it survives until someone rewords a message or a library is updated, and then silently starts classifying everything as the fallback. `Diagnose` checks the tag first and falls back to substrings, so existing call sites keep working while new ones are tagged properly.

## Results

```go
results.OK()             // nothing failed; warnings don't count
results.Worst()          // LevelOK | LevelWarn | LevelError
results.Problems()       // warnings and errors, in check order
results.Log(log.Printf)  // every result, one line each
results.SortedByLevel()  // worst first, for a UI
```

`Run` executes checks **in order, sequentially**. Checks are cheap, and the order they're written in is usually the order that reads best — "is the directory there" before "is the image there" before "can jobs reach docker". Running them concurrently would save milliseconds and scramble the one thing a human reads the output for.

A panicking check becomes an error result rather than taking the program down with it.

## Requirements

- **Go 1.22+** (`go.mod` declares `go 1.22.0`). Nothing here needs a newer
  toolchain, and a library's `go` directive is a hard floor for everyone who
  imports it, so it is kept as low as the code allows. CI tests against 1.22
  and the current release.
- **No dependencies.** The standard library is the whole of it, tests included.
- **Unix only.** `FileGID` reads POSIX ownership through `syscall.Stat_t`, so
  the package does not build on Windows. macOS and Linux both work; the
  behaviour the probes describe — uids, gids, socket ownership — is Linux's.

## Test Coverage

```bash
go test ./... -v

# With coverage — what CI runs
go test -race -coverprofile=coverage.out -covermode=atomic ./...
go tool cover -html=coverage.out -o coverage.html
go tool cover -func=coverage.out
```

Statement coverage is **94.9%**. The test job runs on Linux and macOS, against
Go 1.22 and the current release; one of those combinations uploads the
browsable HTML report as a build artifact. No coverage service is involved.

The runnable examples in `example_test.go` are part of the suite. They are an
*external* test package (`package preflight_test`), so they compile only
against the exported API — which keeps that API honest about being usable from
outside — and `go test` checks their printed output, so they cannot drift from
what the docs claim.

## Changelog

See [CHANGELOG.md](CHANGELOG.md).

## Security

Findings name real paths, uids and addresses on purpose, which makes the output
a log for operators rather than something to publish. Hints are commands, and
nothing here runs them. [SECURITY.md](SECURITY.md) explains what both mean for
a caller, and how to report a vulnerability — please do not open a public issue
for one.

## Contributing

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add some amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## License

Apache 2.0 — see [LICENSE](LICENSE).
