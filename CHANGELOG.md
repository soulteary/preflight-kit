# Changelog

All notable changes to this project are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).
Nothing is tagged yet, so `go get` resolves to a pseudo-version of `main` and
the API may still change.

## [Unreleased]

### Added

- `CHANGELOG.md` and `SECURITY.md`. The security policy covers the two things
  a caller has to get right: results name real paths, uids and addresses on
  purpose, so they are a log rather than a status page; and hints are commands
  this package never runs, which a caller should not run automatically either.
- Runnable examples (`Example`, `ExampleDirWritable`, `ExampleTCPReachable`,
  `ExampleRun_panickingCheck`, `ExampleResults_SortedByLevel`,
  `ExampleClassifier_Diagnose`) that `go test` verifies, so they cannot drift
  from the API. They are an external test package (`package preflight_test`):
  compiling only against the exported API keeps that API honest about being
  sufficient from outside.
- A Go Report Card workflow, run on demand, that regenerates
  `.github/goreportcard.svg` and `.github/goreportcard-report.md` and commits
  them back.
- A `.gitignore` covering build output, coverage artifacts and editor files.

### Changed

- The package doc moved from `preflight.go` to `doc.go`, and gained a layout
  section covering both halves of the package — the startup checks and the
  `Classifier` that diagnoses failures happening later — plus what an
  implementation of `Check` has to hold to.
- CI pins `actions/checkout`, `actions/setup-go` and `actions/upload-artifact`
  to v7, and uploads the HTML coverage report as a build artifact. There is no
  coverage service: the profile is produced and summarised inside the job, and
  the browsable report is downloadable from the run.

## Before the first release

preflight was extracted from the startup self-checks of a GitHub Actions runner
supervisor, where the failures it reports on — a directory the process cannot
write to, a network that no longer exists, a docker socket the user has no
permission on — were being discovered by the first job that needed them rather
than at startup.
