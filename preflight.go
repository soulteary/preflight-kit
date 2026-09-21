package preflight

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// Level is how much a result matters.
type Level string

const (
	// LevelOK means the check passed.
	LevelOK Level = "ok"
	// LevelWarn means it will probably cause trouble later, but the program
	// can start.
	LevelWarn Level = "warn"
	// LevelError means that, as configured, this will not work.
	LevelError Level = "error"
)

// Result is one check's finding.
type Result struct {
	// Name identifies the check, in the operator's vocabulary rather than the
	// code's -- "runners directory", not "checkBasePath".
	Name string

	// Level is the verdict.
	Level Level

	// Message says what was observed. It should name the actual path, address
	// or value involved: "/srv/runners is not writable by uid 1001" sends the
	// reader somewhere, "permission problem" does not.
	Message string

	// Hint is what to do about it, ideally a command to paste. Empty for a
	// passing check.
	Hint string
}

// OK builds a passing result.
func OK(name, message string) Result {
	return Result{Name: name, Level: LevelOK, Message: message}
}

// Warn builds a result for something that will probably cause trouble later.
func Warn(name, message, hint string) Result {
	return Result{Name: name, Level: LevelWarn, Message: message, Hint: hint}
}

// Fail builds a result for something that will not work as configured.
func Fail(name, message, hint string) Result {
	return Result{Name: name, Level: LevelError, Message: message, Hint: hint}
}

// Failed reports whether the result is an error.
func (r Result) Failed() bool { return r.Level == LevelError }

// String renders a result as one line, with the hint appended when there is
// one to give.
func (r Result) String() string {
	marker := "[preflight ok]"
	switch r.Level {
	case LevelWarn:
		marker = "[preflight !]"
	case LevelError:
		marker = "[preflight x]"
	}
	line := fmt.Sprintf("%s %s: %s", marker, r.Name, r.Message)
	if r.Hint != "" && r.Level != LevelOK {
		line += "  -> " + r.Hint
	}
	return line
}

// Check is one self-check. Implementations must be read-only: preflight runs
// on every start, including starts that are already going badly.
type Check interface {
	Run(ctx context.Context) Result
}

// CheckFunc adapts a function to Check.
type CheckFunc func(ctx context.Context) Result

// Run calls f.
func (f CheckFunc) Run(ctx context.Context) Result { return f(ctx) }

// Named wraps a function as a Check that reports under the given name,
// including when it panics or the context is cancelled.
func Named(name string, f func(ctx context.Context) Result) Check {
	return namedCheck{name: name, run: f}
}

type namedCheck struct {
	name string
	run  func(ctx context.Context) Result
}

func (c namedCheck) Run(ctx context.Context) Result {
	if err := ctx.Err(); err != nil {
		return Warn(c.name, "not checked: "+err.Error(),
			"the preflight deadline elapsed before this check ran; raise it, or look into whatever made the earlier checks slow")
	}
	return c.run(ctx)
}

// Name reports the check's name.
func (c namedCheck) Name() string { return c.name }

// Results is an ordered list of findings.
type Results []Result

// Run executes checks in order and collects their results.
//
// In order, and sequentially. Checks are cheap, and the order they are written
// in is usually the order that reads best -- "is the directory there" before
// "is the image there" before "can jobs reach docker". Running them
// concurrently would save milliseconds and scramble the one thing a human
// reads the output for.
//
// A panicking check becomes an error result rather than taking the program
// down with it. A self-check is not worth a crash at startup.
func Run(ctx context.Context, checks ...Check) Results {
	results := make(Results, 0, len(checks))
	for _, c := range checks {
		results = append(results, runOne(ctx, c))
	}
	return results
}

func runOne(ctx context.Context, c Check) (result Result) {
	name := "check"
	if n, ok := c.(interface{ Name() string }); ok && n.Name() != "" {
		name = n.Name()
	}
	defer func() {
		if r := recover(); r != nil {
			result = Fail(name, fmt.Sprintf("the check itself panicked: %v", r),
				"this is a bug in the check, not necessarily in the environment")
		}
	}()
	return c.Run(ctx)
}

// Worst returns the most severe level present, or LevelOK when there is
// nothing to report.
func (rs Results) Worst() Level {
	worst := LevelOK
	for _, r := range rs {
		switch r.Level {
		case LevelError:
			return LevelError
		case LevelWarn:
			worst = LevelWarn
		}
	}
	return worst
}

// OK reports whether nothing failed. Warnings do not count: they are things to
// look at, not reasons to stop.
func (rs Results) OK() bool { return rs.Worst() != LevelError }

// Problems returns the warnings and errors, keeping their original order.
func (rs Results) Problems() Results {
	var out Results
	for _, r := range rs {
		if r.Level != LevelOK {
			out = append(out, r)
		}
	}
	return out
}

// Log writes every result through logf, one line each.
//
// Every result, including the passing ones. The value of a startup self-check
// is as much in "docker daemon 27.0.3, network app-net exists, /srv/data
// writable by uid 1001" as in any single failure: it is the record of what the
// environment looked like, and it is what someone reads first when the same
// deployment misbehaves three weeks later.
func (rs Results) Log(logf func(format string, args ...any)) {
	for _, r := range rs {
		logf("%s", r.String())
	}
}

// String renders all results, one per line.
func (rs Results) String() string {
	lines := make([]string, 0, len(rs))
	for _, r := range rs {
		lines = append(lines, r.String())
	}
	return strings.Join(lines, "\n")
}

// SortedByLevel returns the results with errors first, then warnings, then
// passes, preserving the original order within each group.
//
// For a UI that shows the worst first. Log output should stay in check order.
func (rs Results) SortedByLevel() Results {
	out := make(Results, len(rs))
	copy(out, rs)
	rank := map[Level]int{LevelError: 0, LevelWarn: 1, LevelOK: 2}
	sort.SliceStable(out, func(i, j int) bool { return rank[out[i].Level] < rank[out[j].Level] })
	return out
}
