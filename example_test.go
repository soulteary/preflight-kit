package preflight_test

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"time"

	preflight "github.com/soulteary/preflight-kit"
)

// The common case: run the checks at startup, log every result, and carry on.
// A failing check never stops the program -- reporting is the whole job.
func Example() {
	results := preflight.Run(context.Background(),
		preflight.Named("data directory", func(context.Context) preflight.Result {
			return preflight.OK("data directory", "/srv/app/data is writable (uid 1001)")
		}),
		preflight.Named("docker socket", func(context.Context) preflight.Result {
			return preflight.Warn("docker socket",
				"/var/run/docker.sock is owned by gid 998 and this process is not in that group; access will be denied",
				`add the process to gid 998 (docker-compose: group_add: ["998"])`)
		}),
	)

	results.Log(func(format string, args ...any) { fmt.Printf(format+"\n", args...) })
	fmt.Println("ok:", results.OK(), "worst:", results.Worst())

	// Output:
	// [preflight ok] data directory: /srv/app/data is writable (uid 1001)
	// [preflight !] docker socket: /var/run/docker.sock is owned by gid 998 and this process is not in that group; access will be denied  -> add the process to gid 998 (docker-compose: group_add: ["998"])
	// ok: true worst: warn
}

// DirWritable really writes: it creates a randomly named probe file and removes
// it again. Both halves matter -- a directory that accepts a new file and
// refuses to let it be deleted is how a read-only remount first shows itself,
// and a check that stops at "created it" calls that directory fine.
func ExampleDirWritable() {
	dir, err := os.MkdirTemp("", "preflight-example-")
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	usable := preflight.DirWritable("data directory", dir)
	fmt.Println(usable.Level, usable.Failed())

	missing := preflight.DirWritable("data directory", filepath.Join(dir, "does-not-exist"))
	fmt.Println(missing.Level, missing.Failed())

	// Output:
	// ok false
	// error true
}

// TCPReachable answers "does the address answer", which is the only half that
// means anything: a dependency that is configured but down looks exactly like
// one that is configured and up. An unreachable dependency is a warning, not an
// error -- something to look at, not a reason to refuse to start.
func ExampleTCPReachable() {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}
	addr := listener.Addr().String()

	up := preflight.TCPReachable(context.Background(), "database", addr, time.Second)
	fmt.Println(up.Level, up.Failed())

	_ = listener.Close()
	down := preflight.TCPReachable(context.Background(), "database", addr, time.Second)
	fmt.Println(down.Level, down.Failed())

	// Output:
	// ok false
	// warn false
}

// A panicking check becomes an error result, under its own name, rather than
// taking the program down with it. A self-check is not worth a crash at
// startup.
func ExampleRun_panickingCheck() {
	results := preflight.Run(context.Background(),
		preflight.Named("image present", func(context.Context) preflight.Result {
			panic("read of a nil map")
		}),
	)

	fmt.Println(results[0].Name, results[0].Level)
	fmt.Println(results[0].Message)

	// Output:
	// image present error
	// the check itself panicked: read of a nil map
}

// SortedByLevel is for a UI that shows the worst first. Log output stays in
// check order, because the order the checks are written in is usually the order
// that reads best.
func ExampleResults_SortedByLevel() {
	results := preflight.Results{
		preflight.OK("data directory", "/srv/app/data is writable"),
		preflight.Warn("docker socket", "this process is not in the owning group", `group_add: ["998"]`),
		preflight.OK("network", "app-net exists"),
		preflight.Fail("image", "app:v2 is not present locally", "docker pull app:v2"),
	}

	for _, r := range results.SortedByLevel() {
		fmt.Println(r.Level, r.Name)
	}

	// Output:
	// error image
	// warn docker socket
	// ok data directory
	// ok network
}

// Diagnose answers the other half of the question: when something fails later,
// what kind of failure is it and what should be done about it. A kind tagged at
// the point of failure is matched first; matching on the message text is only
// the fallback, because that goes wrong quietly when someone rewords a message.
//
// CheckCommand and FixCommand are separate so the read-only one can always be
// offered first.
func ExampleClassifier_Diagnose() {
	classifier := preflight.Classifier{
		Rules: []preflight.Rule{{
			Kind:       "docker-permission",
			Substrings: []string{"permission denied while trying to connect"},
			Advice: preflight.Advice{
				Suggestion:   "this process is not in the group that owns the docker socket",
				CheckCommand: "stat -c '%g' /var/run/docker.sock",
				FixCommand:   "usermod -aG docker app && systemctl restart app",
			},
		}},
		Fallback: preflight.Advice{Suggestion: "no advice for this failure yet"},
	}

	// Tagged where it happened, so the message text is never consulted.
	tagged := preflight.Wrap("docker-permission", errors.New("dial unix /var/run/docker.sock: connect: permission denied"))
	known := classifier.Diagnose(tagged)
	fmt.Println(known.Kind)
	fmt.Println(known.CheckCommand)

	unknown := classifier.Diagnose(errors.New("no space left on device"))
	fmt.Println(unknown.Kind, "-", unknown.Suggestion)

	// Output:
	// docker-permission
	// stat -c '%g' /var/run/docker.sock
	// unknown - no advice for this failure yet
}
