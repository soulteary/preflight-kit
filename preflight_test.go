package preflight

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// --- results ---

func TestBuildersAndLevels(t *testing.T) {
	if r := OK("db", "reachable"); r.Level != LevelOK || r.Failed() || r.Hint != "" {
		t.Errorf("OK = %+v", r)
	}
	if r := Warn("db", "slow", "check the pool"); r.Level != LevelWarn || r.Failed() {
		t.Errorf("Warn = %+v", r)
	}
	if r := Fail("db", "down", "start it"); r.Level != LevelError || !r.Failed() {
		t.Errorf("Fail = %+v", r)
	}
}

// A finding that does not say what to do has moved the work, not done it.
func TestStringAppendsTheHintOnlyWhenActionable(t *testing.T) {
	if got := Fail("dir", "not writable", "chown -R 1001 /srv").String(); !strings.Contains(got, "-> chown -R 1001 /srv") {
		t.Errorf("an error should carry its hint: %q", got)
	}
	// A passing check has nothing to suggest, even if a hint was set.
	got := Result{Name: "dir", Level: LevelOK, Message: "fine", Hint: "chown"}.String()
	if strings.Contains(got, "chown") {
		t.Errorf("a passing check should not offer a fix: %q", got)
	}
}

func TestWorstAndOK(t *testing.T) {
	cases := []struct {
		name  string
		rs    Results
		worst Level
		ok    bool
	}{
		{"empty", nil, LevelOK, true},
		{"all ok", Results{OK("a", "")}, LevelOK, true},
		// Warnings are things to look at, not reasons to stop.
		{"warn", Results{OK("a", ""), Warn("b", "", "")}, LevelWarn, true},
		{"error", Results{Warn("a", "", ""), Fail("b", "", "")}, LevelError, false},
		{"error first", Results{Fail("a", "", ""), Warn("b", "", "")}, LevelError, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.rs.Worst(); got != tc.worst {
				t.Errorf("Worst = %q, want %q", got, tc.worst)
			}
			if got := tc.rs.OK(); got != tc.ok {
				t.Errorf("OK = %v, want %v", got, tc.ok)
			}
		})
	}
}

func TestProblemsKeepsOrder(t *testing.T) {
	rs := Results{OK("a", ""), Fail("b", "", ""), OK("c", ""), Warn("d", "", "")}
	got := rs.Problems()
	if len(got) != 2 || got[0].Name != "b" || got[1].Name != "d" {
		t.Fatalf("Problems = %+v", got)
	}
}

// Log keeps check order; SortedByLevel is for a UI that shows the worst first.
func TestSortedByLevelIsStable(t *testing.T) {
	rs := Results{OK("a", ""), Warn("b", "", ""), Fail("c", "", ""), Warn("d", "", ""), Fail("e", "", "")}
	var names []string
	for _, r := range rs.SortedByLevel() {
		names = append(names, r.Name)
	}
	want := []string{"c", "e", "b", "d", "a"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("SortedByLevel = %v, want %v", names, want)
	}
	// The original must not be reordered underneath the caller.
	if rs[0].Name != "a" {
		t.Fatal("SortedByLevel modified its receiver")
	}
}

// The passing lines are the record of what the environment looked like, which
// is what someone reads first weeks later.
func TestLogWritesEveryResult(t *testing.T) {
	var lines []string
	Results{OK("a", "fine"), Warn("b", "hmm", "look"), Fail("c", "no", "fix")}.
		Log(func(format string, args ...any) { lines = append(lines, fmt.Sprintf(format, args...)) })

	if len(lines) != 3 {
		t.Fatalf("logged %d lines, want 3", len(lines))
	}
	if !strings.Contains(lines[0], "a: fine") {
		t.Errorf("passing checks belong in the log too: %q", lines[0])
	}
}

// --- running ---

func TestRunKeepsCheckOrder(t *testing.T) {
	var checks []Check
	for _, n := range []string{"first", "second", "third"} {
		checks = append(checks, Named(n, func(context.Context) Result { return OK(n, "") }))
	}
	rs := Run(context.Background(), checks...)
	if len(rs) != 3 || rs[0].Name != "first" || rs[2].Name != "third" {
		t.Fatalf("Run = %+v", rs)
	}
}

// A self-check is not worth a crash at startup.
func TestRunRecoversFromAPanickingCheck(t *testing.T) {
	rs := Run(context.Background(),
		Named("good", func(context.Context) Result { return OK("good", "") }),
		Named("bad", func(context.Context) Result { panic("boom") }),
		Named("after", func(context.Context) Result { return OK("after", "") }),
	)
	if len(rs) != 3 {
		t.Fatalf("Run = %+v, want all three", rs)
	}
	if !rs[1].Failed() || !strings.Contains(rs[1].Message, "panicked") {
		t.Errorf("the panicking check should become an error result: %+v", rs[1])
	}
	if rs[1].Name != "bad" {
		t.Errorf("the result should keep the check's name, got %q", rs[1].Name)
	}
	if rs[2].Name != "after" || rs[2].Failed() {
		t.Errorf("checks after a panic should still run: %+v", rs[2])
	}
}

func TestNamedReportsACancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	rs := Run(ctx, Named("slow", func(context.Context) Result {
		t.Error("the check body should not run once the context is done")
		return OK("slow", "")
	}))
	if len(rs) != 1 || rs[0].Level != LevelWarn || rs[0].Name != "slow" {
		t.Fatalf("Run = %+v, want a warning naming the check", rs)
	}
}

func TestCheckFunc(t *testing.T) {
	rs := Run(context.Background(), CheckFunc(func(context.Context) Result { return OK("x", "ok") }))
	if len(rs) != 1 || rs[0].Name != "x" {
		t.Fatalf("Run = %+v", rs)
	}
}

// --- probes ---

func TestDirWritable(t *testing.T) {
	dir := t.TempDir()
	if r := DirWritable("data", dir); r.Level != LevelOK {
		t.Fatalf("DirWritable(%s) = %+v", dir, r)
	}

	missing := filepath.Join(dir, "nope")
	r := DirWritable("data", missing)
	if !r.Failed() || !strings.Contains(r.Message, missing) {
		t.Errorf("a missing directory should fail and name itself: %+v", r)
	}
	if r.Hint == "" {
		t.Error("a failing directory check should suggest something")
	}

	file := filepath.Join(dir, "a-file")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if r := DirWritable("data", file); !r.Failed() {
		t.Errorf("a file is not a directory: %+v", r)
	}
}

// The probe file must not be able to collide with real data.
func TestDirWritableLeavesNothingBehind(t *testing.T) {
	dir := t.TempDir()
	keep := filepath.Join(dir, "important.yaml")
	if err := os.WriteFile(keep, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	for range 5 {
		if r := DirWritable("data", dir); r.Level != LevelOK {
			t.Fatalf("DirWritable = %+v", r)
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "important.yaml" {
		t.Fatalf("the probe left files behind: %v", entries)
	}
	if b, _ := os.ReadFile(keep); string(b) != "data" {
		t.Fatal("the probe overwrote an existing file")
	}
}

func TestPathExists(t *testing.T) {
	f := filepath.Join(t.TempDir(), "x")
	if err := os.WriteFile(f, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if r := PathExists("socket", f, ""); r.Level != LevelOK {
		t.Errorf("PathExists = %+v", r)
	}
	r := PathExists("socket", f+".missing", "mount it")
	if !r.Failed() || r.Hint != "mount it" {
		t.Errorf("PathExists = %+v", r)
	}
}

// "Configured" and "answers" get conflated, and only the second means
// anything.
func TestTCPReachable(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()

	if r := TCPReachable(context.Background(), "dep", ln.Addr().String(), time.Second); r.Level != LevelOK {
		t.Errorf("a listening address should pass: %+v", r)
	}

	addr := ln.Addr().String()
	_ = ln.Close()
	r := TCPReachable(context.Background(), "dep", addr, 200*time.Millisecond)
	if r.Level != LevelWarn || !strings.Contains(r.Message, addr) {
		t.Errorf("a closed address should warn and name itself: %+v", r)
	}
}

// Unbounded, an address that blackholes packets hangs far longer than anyone
// waits at startup.
func TestTCPReachableIsAlwaysBounded(t *testing.T) {
	start := time.Now()
	// TEST-NET-1 (RFC 5737): routable nowhere.
	r := TCPReachable(context.Background(), "dep", "192.0.2.1:9", 150*time.Millisecond)
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("took %v; the timeout was not applied", elapsed)
	}
	if r.Level != LevelWarn {
		t.Errorf("an unreachable address should warn: %+v", r)
	}
	// A zero timeout must still be bounded, by the default.
	start = time.Now()
	TCPReachable(context.Background(), "dep", "192.0.2.1:9", 0)
	if elapsed := time.Since(start); elapsed > DefaultDialTimeout+2*time.Second {
		t.Fatalf("a zero timeout was not replaced by the default: %v", elapsed)
	}
}

func TestFileGIDAndInGroup(t *testing.T) {
	if gid := FileGID("/definitely/not/here"); gid != -1 {
		t.Errorf("FileGID(missing) = %d, want -1", gid)
	}
	f := filepath.Join(t.TempDir(), "x")
	if err := os.WriteFile(f, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	gid := FileGID(f)
	if gid < 0 {
		t.Fatalf("FileGID = %d", gid)
	}
	if !InGroup(gid) {
		t.Errorf("a file this process just created should belong to one of its groups (gid %d)", gid)
	}
	if InGroup(1 << 30) {
		t.Error("InGroup returned true for an implausible gid")
	}
}

func TestSocketAccessible(t *testing.T) {
	r := SocketAccessible("docker", "/definitely/not/here.sock")
	if !r.Failed() || r.Hint == "" {
		t.Errorf("a missing socket should fail with a hint: %+v", r)
	}

	sock := filepath.Join(t.TempDir(), "x.sock")
	if err := os.WriteFile(sock, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if r := SocketAccessible("docker", sock); r.Level != LevelOK {
		t.Errorf("a socket owned by our own group should pass: %+v", r)
	}
}

// Reports, never changes: tightening permissions can break a deployment whose
// uids do not line up the way the checker assumes.
func TestSubdirsPrivate(t *testing.T) {
	base := t.TempDir()
	tight := filepath.Join(base, "tight")
	loose := filepath.Join(base, "loose")
	for dir, mode := range map[string]os.FileMode{tight: 0o700, loose: 0o755} {
		if err := os.Mkdir(dir, mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(dir, mode); err != nil {
			t.Fatal(err)
		}
	}

	if r := SubdirsPrivate("dirs", []string{tight}); r.Level != LevelOK {
		t.Errorf("a 0700 directory should pass: %+v", r)
	}

	r := SubdirsPrivate("dirs", []string{tight, loose, filepath.Join(base, "absent")})
	if r.Level != LevelWarn {
		t.Fatalf("a 0755 directory should warn: %+v", r)
	}
	if !strings.Contains(r.Message, loose) || strings.Contains(r.Message, tight) {
		t.Errorf("only the loose directory should be named: %q", r.Message)
	}
	if !strings.HasPrefix(r.Hint, "chmod 700 ") {
		t.Errorf("the hint should be a command to paste: %q", r.Hint)
	}

	// Permissions were reported, not changed.
	info, err := os.Stat(loose)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("the check altered permissions: %v", info.Mode().Perm())
	}
}

func TestCommandsPresent(t *testing.T) {
	if r := CommandsPresent("tools", "the image", nil); r.Level != LevelOK {
		t.Errorf("CommandsPresent(none missing) = %+v", r)
	}
	r := CommandsPresent("tools", "the image", []string{"git", "unzip"})
	if r.Level != LevelWarn || !strings.Contains(r.Message, "git unzip") {
		t.Errorf("CommandsPresent = %+v", r)
	}
}

// --- diagnosis ---

var classifier = Classifier{
	Rules: []Rule{
		{
			Kind:       "docker-access",
			Substrings: []string{"permission denied", "cannot connect to the docker daemon"},
			Advice: Advice{
				Suggestion:   "check the socket mount and group membership",
				CheckCommand: "ls -l /var/run/docker.sock && id",
				FixCommand:   "usermod -aG docker $USER && systemctl restart docker",
			},
		},
		{
			Kind:       "agent-connect",
			Substrings: []string{"connection refused", "no such host"},
			Advice:     Advice{Suggestion: "check the container network", CheckCommand: "docker network inspect app-net"},
		},
	},
	Fallback: Advice{Suggestion: "look at the logs", CheckCommand: "docker compose ps"},
}

func TestDiagnosePrefersTheTaggedKind(t *testing.T) {
	// The text says one thing, the tag says another: the tag wins, because
	// it was applied where the failure happened.
	err := Wrap("agent-connect", errors.New("permission denied"))
	d := classifier.Diagnose(err)
	if d.Kind != "agent-connect" {
		t.Fatalf("Kind = %q, want agent-connect (the tag, not the text)", d.Kind)
	}
	if d.CheckCommand != "docker network inspect app-net" {
		t.Errorf("advice = %+v", d.Advice)
	}
	if d.Error != "permission denied" {
		t.Errorf("the original message should be kept verbatim: %q", d.Error)
	}
}

// Text matching is the fallback so existing call sites keep working while new
// ones are tagged properly.
func TestDiagnoseFallsBackToSubstrings(t *testing.T) {
	d := classifier.Diagnose(errors.New("Got permission denied while trying to connect"))
	if d.Kind != "docker-access" {
		t.Fatalf("Kind = %q", d.Kind)
	}
	if d.FixCommand == "" {
		t.Error("docker-access should offer a fix command")
	}
}

// A safe command must always be offered before a destructive one.
func TestCheckAndFixCommandsAreSeparate(t *testing.T) {
	d := classifier.Diagnose(errors.New("cannot connect to the docker daemon"))
	if d.CheckCommand == "" {
		t.Fatal("there should always be something safe to run first")
	}
	if d.CheckCommand == d.FixCommand {
		t.Fatal("the read-only and the mutating command must not be the same field")
	}
}

func TestDiagnoseUnmatched(t *testing.T) {
	d := classifier.Diagnose(errors.New("something else entirely"))
	if d.Kind != DefaultFallbackKind || d.Suggestion != "look at the logs" {
		t.Fatalf("Diagnose = %+v", d)
	}

	custom := Classifier{FallbackKind: "unclassified"}
	if got := custom.Diagnose(errors.New("x")).Kind; got != "unclassified" {
		t.Errorf("FallbackKind = %q", got)
	}
}

// A kind the classifier does not know is kept -- the caller meant something by
// it -- but no advice is invented for it.
func TestDiagnoseKeepsAnUnknownKind(t *testing.T) {
	d := classifier.Diagnose(Wrap("brand-new-kind", errors.New("x")))
	if d.Kind != "brand-new-kind" {
		t.Fatalf("Kind = %q", d.Kind)
	}
	if d.Suggestion != classifier.Fallback.Suggestion {
		t.Errorf("an unknown kind should fall back rather than invent advice: %+v", d.Advice)
	}
}

func TestDiagnoseNil(t *testing.T) {
	d := classifier.Diagnose(nil)
	if d.Error != "" || d.Kind != DefaultFallbackKind {
		t.Fatalf("Diagnose(nil) = %+v", d)
	}
}

func TestWrapAndKindOf(t *testing.T) {
	if Wrap("k", nil) != nil {
		t.Fatal("Wrap(nil) should be nil so it can be applied unconditionally")
	}

	base := errors.New("boom")
	err := Wrap("k", base)
	if KindOf(err) != "k" {
		t.Errorf("KindOf = %q", KindOf(err))
	}
	if !errors.Is(err, base) {
		t.Error("Wrap should keep the cause reachable")
	}
	if err.Error() != "boom" {
		t.Errorf("Error() = %q, want the underlying message", err.Error())
	}
	if KindOf(base) != "" {
		t.Error("an untagged error should have no kind")
	}
	// Reachable through another layer of wrapping.
	if KindOf(fmt.Errorf("context: %w", err)) != "k" {
		t.Error("KindOf should see through wrapping")
	}

	var nilErr *Error
	if nilErr.Error() != "" || nilErr.Unwrap() != nil {
		t.Error("a nil *Error should be harmless")
	}
}

func TestDir(t *testing.T) {
	if got := Dir("a", "b", "c"); got != filepath.Join("a", "b", "c") {
		t.Errorf("Dir = %q", got)
	}
}
