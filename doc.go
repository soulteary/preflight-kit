// Package preflight runs read-only self-checks at startup and reports what it
// found in a form an operator can act on without a second round trip.
//
// The problem it addresses is timing. Most deployment mistakes -- a directory
// the process cannot write to, a network that no longer exists, a socket the
// user has no permission on -- are perfectly detectable the moment the process
// starts, and are instead discovered much later, by the first piece of real
// work that needs them. By then the failure surfaces as whatever that work
// happens to fail with, at a moment nobody is watching, in a message that says
// nothing about the setting that caused it.
//
// So a [Result] carries more than a verdict. [Result.Message] says what was
// observed; [Result.Hint] is a command the reader can paste. A check that
// reports a problem without saying what to do about it has moved the work
// rather than done it.
//
// Checks are read-only, and a failing check never stops the program: reporting
// is the whole job. A startup self-check that refuses to start is a self-check
// that will be removed the first time it is wrong about an environment its
// author did not anticipate.
//
// # Layout
//
// The package has no dependencies beyond the standard library, and splits into
// two halves that share one purpose -- neither is finished until it has said
// what to do about what it found.
//
// Startup: [Check], [Run] and [Results] are the harness; [Named] and
// [CheckFunc] adapt a function to it; [OK], [Warn] and [Fail] build the
// verdicts. The built-in probes -- [DirWritable], [PathExists], [TCPReachable],
// [SocketAccessible], [SubdirsPrivate] and [MissingCommands] -- cover the
// settings that go wrong most often.
//
// Afterwards: [Classifier] turns a runtime failure into a [Diagnosis], so the
// same question ("what should I do about this?") gets an answer when something
// breaks later rather than at startup. [Wrap] and [KindOf] carry a failure's
// kind from where it happens to where it is reported.
//
// # Getting started
//
//	results := preflight.Run(ctx,
//		preflight.Named("data directory", func(ctx context.Context) preflight.Result {
//			return preflight.DirWritable("data directory", cfg.DataDir)
//		}),
//		preflight.Named("database", func(ctx context.Context) preflight.Result {
//			return preflight.TCPReachable(ctx, "database", cfg.DBAddr, 3*time.Second)
//		}),
//	)
//	results.Log(log.Printf)
//	if !results.OK() {
//		log.Printf("starting anyway; the findings above are worth fixing")
//	}
//
// [Results.Log] writes every result, including the passing ones: the record of
// what the environment looked like at startup is what someone reads first when
// the same deployment misbehaves three weeks later. [Results.OK] reports
// whether anything failed -- warnings do not count, being things to look at
// rather than reasons to stop -- and what a caller does with that answer is
// its own decision, because this package never makes it.
//
// # Writing a check
//
// A check is any function of a context returning a [Result]; [Named] gives it
// a name and makes it report under that name even when it panics or the
// preflight deadline elapses first. A Check with a type of its own gets the
// same treatment by implementing [NamedCheck]. Implementations must be read-only, because
// preflight runs on every start, including starts that are already going
// badly. The one deliberate exception is [DirWritable], which creates and
// removes a randomly named probe file -- the only way to learn whether a
// directory really accepts writes is to write.
//
// Checks run in the order they are given, sequentially. Running them
// concurrently would save milliseconds and scramble the one thing a human
// reads the output for.
package preflight
