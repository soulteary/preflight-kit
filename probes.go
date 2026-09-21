package preflight

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// DirWritable checks that dir exists, is a directory, and that this process
// can both create and remove a file in it.
//
// Both, not just create. A directory can accept a new file and refuse to let
// you delete it -- a sticky bit, or a mount that turns read-only under the
// first write -- and a check that stops at "created it" calls that directory
// fine. The program then fails later, on the first thing that needs to replace
// or clean up a file, with no connection back to this setting.
//
// The probe file has a random name. A fixed one collides with a real file of
// that name, and a self-check that opens and deletes the user's data is worse
// than no self-check at all.
func DirWritable(name, dir string) Result {
	info, err := os.Stat(dir)
	if err != nil {
		return Fail(name, fmt.Sprintf("%s is not accessible: %v", dir, err),
			fmt.Sprintf("mkdir -p %s && chown %d:%d %s", dir, os.Getuid(), os.Getgid(), dir))
	}
	if !info.IsDir() {
		return Fail(name, fmt.Sprintf("%s is not a directory", dir), "")
	}

	f, err := os.CreateTemp(dir, ".preflight-write-test-*")
	if err != nil {
		return Fail(name,
			fmt.Sprintf("%s is not writable by the current user (uid %d): %v", dir, os.Getuid(), err),
			fmt.Sprintf("chown -R %d:%d %s", os.Getuid(), os.Getgid(), dir))
	}
	probe := f.Name()
	_ = f.Close()

	if err := os.Remove(probe); err != nil {
		return Fail(name,
			fmt.Sprintf("%s accepts new files but will not let them be removed (%s is left behind): %v", dir, probe, err),
			fmt.Sprintf("check the directory's permissions and mount options; uid %d needs full read-write access", os.Getuid()))
	}
	return OK(name, fmt.Sprintf("%s is writable (uid %d)", dir, os.Getuid()))
}

// PathExists checks that a path exists, without saying anything about what
// can be done with it.
func PathExists(name, path, hint string) Result {
	if _, err := os.Stat(path); err != nil {
		return Fail(name, fmt.Sprintf("%s does not exist: %v", path, err), hint)
	}
	return OK(name, path+" exists")
}

// DefaultDialTimeout bounds a TCPReachable probe that is given none.
const DefaultDialTimeout = 3 * time.Second

// TCPReachable checks that something is listening on addr.
//
// It exists because "the address is configured" and "the address answers" get
// conflated, and only the second one means anything. A check that reports the
// configured value back to the reader, in green, has told them nothing: a
// dependency that is configured but down looks exactly the same.
//
// Always bounded. An unreachable address that blackholes packets hangs until
// the TCP stack gives up, which is far longer than anyone will wait at
// startup for a check that is meant to be reassuring.
func TCPReachable(ctx context.Context, name, addr string, timeout time.Duration) Result {
	if timeout <= 0 {
		timeout = DefaultDialTimeout
	}
	dialCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var d net.Dialer
	conn, err := d.DialContext(dialCtx, "tcp", addr)
	if err != nil {
		return Warn(name, fmt.Sprintf("%s is not reachable: %v", addr, err),
			"check that the service is running and that this process can reach it")
	}
	_ = conn.Close()
	return OK(name, addr+" is reachable")
}

// FileGID returns the group that owns path, or -1 when that cannot be
// determined.
func FileGID(path string) int {
	info, err := os.Stat(path)
	if err != nil {
		return -1
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return -1
	}
	return int(st.Gid)
}

// InGroup reports whether this process belongs to gid, as its primary group
// or a supplementary one.
func InGroup(gid int) bool {
	if os.Getgid() == gid {
		return true
	}
	groups, err := os.Getgroups()
	if err != nil {
		return false
	}
	for _, g := range groups {
		if g == gid {
			return true
		}
	}
	return false
}

// SocketAccessible checks that a unix socket exists and that this process is
// in the group that owns it.
//
// The group membership is the part worth checking. The socket being present is
// easy to see and easy to get right; being in its group is neither, and it is
// the half that produces "permission denied" from inside a container long
// after everyone has concluded the mount is correct.
func SocketAccessible(name, path string) Result {
	if _, err := os.Stat(path); err != nil {
		return Fail(name, fmt.Sprintf("%s does not exist: %v", path, err),
			fmt.Sprintf("mount it into this process, e.g. -v %s:%s", path, path))
	}
	gid := FileGID(path)
	if gid < 0 {
		return Warn(name, fmt.Sprintf("%s exists but its owning group could not be determined", path), "")
	}
	if !InGroup(gid) {
		return Warn(name,
			fmt.Sprintf("%s is owned by gid %d and this process is not in that group; access will be denied", path, gid),
			fmt.Sprintf("add the process to gid %d (docker-compose: group_add: [\"%d\"])", gid, gid))
	}
	return OK(name, fmt.Sprintf("%s is accessible (gid %d)", path, gid))
}

// SubdirsPrivate reports the directories under a parent whose permissions let
// other users in.
//
// Separate from DirWritable because it answers a different question -- not
// "can I use this" but "can anyone else read what I put here". It reports and
// does not change anything: tightening permissions can break a deployment
// whose uids do not line up the way the checker assumes, so the decision
// belongs to whoever can see the whole picture.
func SubdirsPrivate(name string, dirs []string) Result {
	var loose []string
	for _, dir := range dirs {
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			continue
		}
		if info.Mode().Perm()&0o077 != 0 {
			loose = append(loose, dir)
		}
	}
	if len(loose) == 0 {
		return OK(name, "no directory is readable by other users")
	}
	return Warn(name,
		fmt.Sprintf("%d directories can be entered by other users on this host: %s",
			len(loose), joinPaths(loose)),
		"chmod 700 "+joinPaths(loose))
}

func joinPaths(paths []string) string {
	out := ""
	for i, p := range paths {
		if i > 0 {
			out += " "
		}
		out += p
	}
	return out
}

// CommandsPresent turns the result of a "which of these commands exist" probe
// into a Result. The caller supplies the missing list, because how to ask
// depends on where the commands have to be -- this host, a container image, a
// remote machine.
//
// Missing tools deserve a check of their own because of how they fail: not
// with "command not found" at a useful moment, but as whatever the thing
// calling them does when it is absent. A build step that silently downloads a
// tarball instead of cloning, because git is missing, looks like a success
// until someone wonders why the working directory has no history.
func CommandsPresent(name, where string, missing []string) Result {
	if len(missing) == 0 {
		return OK(name, where+" has every required command")
	}
	return Warn(name,
		fmt.Sprintf("%s is missing %s; anything that needs them will fail at the point of use, not here",
			where, joinPaths(missing)),
		"install them in "+where)
}

// Dir joins the elements, for callers building probe paths.
func Dir(elem ...string) string { return filepath.Join(elem...) }
