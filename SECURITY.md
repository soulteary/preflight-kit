# Security Policy

## Reporting a vulnerability

**Please do not open a public issue for a security problem.**

Report it through GitHub's private vulnerability reporting: go to the
[Security tab](https://github.com/soulteary/preflight-kit/security) and choose
**Report a vulnerability**. That opens a private advisory visible only to the
maintainers.

If you do not see that option, open a normal issue saying only that you have a
security report and need a private channel — **no details, no reproducer** —
and a maintainer will arrange one.

Please include, once you have a private channel:

- the affected version or commit,
- what an attacker can do, and what they need in order to do it,
- a reproducer, if you have one.

Expect an acknowledgement within a few days. This is a small
volunteer-maintained project, so please allow reasonable time for a fix before
disclosing publicly.

## Supported versions

| Version | Supported |
| ------- | --------- |
| 1.x     | ✅ |

Fixes land on the latest minor of the current major. There are no long-term
support branches. Until `v1.0.0` is tagged, `main` is that version.

## What this library does, and what it does not

preflight-kit reports on the environment a process is starting in. Two of its
properties are security-relevant, and both are about what the *caller* does
with the results.

### Results describe your infrastructure, on purpose

A finding names the real path, uid, gid or address involved — `/srv/runners is
not writable by uid 1001`, `/var/run/docker.sock is owned by gid 998` — because
a message that does not send the reader somewhere is not worth printing.

That makes the output a log for operators, not a status page. Rendering
`Results.String()` into a public health endpoint, an unauthenticated admin UI
or a support bundle you hand to a third party publishes your filesystem layout,
your uid/gid assignments and the addresses of your dependencies.

The same applies to `Diagnosis.Error`, which keeps the underlying error text
verbatim.

### Hints are commands, and this package never runs them

`Result.Hint` and `Advice.FixCommand` hold text like `chown -R 1001:1001 /srv`,
`chmod 700 …` and `usermod -aG docker app`. They exist so a human can read and
paste them. **Nothing in this package executes them, and a caller should not
execute them either.**

A "fix it for me" button turns a probe's opinion about a path into a privileged
operation on that path. The paths come from your configuration, so whoever can
edit the configuration — or create a directory where the probe looks — chooses
what the command is applied to.

`Advice` splits `CheckCommand` from `FixCommand` for this reason: the
read-only one is safe to offer anywhere, and the two are separate fields so
nobody has to choose between a safe command and a useful one.

### The checks are read-only, with one deliberate exception

`DirWritable` creates a file and removes it again. That is the only way to
learn whether a directory really accepts writes, and it is the whole point of
the probe. The file has a **random** name (`os.CreateTemp` with a
`.preflight-write-test-*` pattern): a fixed name would collide with a real file
of that name, and a self-check that opens and deletes the user's data is worse
than no self-check at all.

If the probe file cannot be removed it is reported, by name, as an error — it
is left behind, and the message says so.

`SubdirsPrivate` reports directories other users can enter and **changes
nothing**. Tightening permissions can break a deployment whose uids do not line
up the way the checker assumes, so that decision belongs to whoever can see the
whole picture.

`TCPReachable` opens a TCP connection to whatever address it is given and
closes it immediately. It is always bounded — `DefaultDialTimeout` when the
caller passes none — because an address that blackholes packets otherwise hangs
until the TCP stack gives up.

### This is not an access-control gate

`Results.OK()` is information. A failing check never stops the program, and
warnings do not even count as failures. If something must not start under a
given condition, the caller enforces that; do not rely on preflight to do it,
and do not add a check that refuses to start — a self-check that blocks startup
is one that gets deleted the first time it is wrong about an environment its
author did not anticipate.

### Classify at the point of failure, not by message text

`Classifier` matches `Rule.Kind` first and `Rule.Substrings` only as a
fallback. Substring matching is guesswork that fails quietly: it survives until
someone rewords a message or a dependency is updated, and then silently starts
classifying everything as the fallback — including failures whose advice
mattered. Prefer `Wrap(kind, err)` where the failure happens.
