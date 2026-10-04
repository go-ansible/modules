package modules

import (
	"context"
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"time"

	remoteexec "github.com/go-remoteexec/transport"
)

// Real Ansible's own convention for where a backgrounded job's status
// lives on the target — reused here (not because anything reads it
// back with real Ansible's own tooling, but because it's a reasonable,
// familiar default) via $HOME rather than a literal "~" (portable
// across every /bin/sh this might run under, with no reliance on tilde
// expansion rules that can differ subtly between shells).
const asyncDirExpr = `"$HOME/.ansible_async"`

// AsyncLaunch backgrounds cmdLine on conn's target under a fresh job
// ID, returning immediately without waiting for it to finish — the
// command keeps running independently of this call and of the
// connection itself (nohup traps SIGHUP, so it survives the
// connection closing), which is the entire point of async:.
//
// A real, disclosed limitation: unlike real Ansible's own async
// wrapper (which forks/setpgids the job so it can SIGKILL the whole
// process group if async: 's time limit is exceeded), this does NOT
// actively kill a job that overruns its time limit — there is no
// portable POSIX shell equivalent to killpg across every target shell
// this might run against without a dependency (setsid, part of
// util-linux) that real targets (macOS/BSD in particular) do not ship
// by default. AsyncCheck (see below) still enforces the time limit on
// the CONTROLLER side (a poll loop gives up and reports a timeout
// failure once the limit passes), it just doesn't reach out and stop
// the job itself the way real Ansible's wrapper does.
func AsyncLaunch(ctx context.Context, conn remoteexec.Connection, cmdLine string) (jid string, err error) {
	jid = strconv.FormatInt(time.Now().UnixNano(), 10) + "." + strconv.Itoa(rand.Intn(1_000_000))
	cmdDelim := fmt.Sprintf("ANSIBLE_ASYNC_CMD_%d", rand.Int63())

	script := fmt.Sprintf(`d=%s/%s
mkdir -p "$d"
cat > "$d/cmd.sh" <<'%s'
%s
%s
cat > "$d/runner.sh" <<RUNNER_EOF
#!/bin/sh
"$d/cmd.sh" >"$d/stdout" 2>"$d/stderr"
echo \$? >"$d/rc.tmp"
mv "$d/rc.tmp" "$d/rc"
RUNNER_EOF
chmod +x "$d/cmd.sh" "$d/runner.sh"
nohup "$d/runner.sh" >/dev/null 2>&1 &
echo %s
`, asyncDirExpr, jid, cmdDelim, cmdLine, cmdDelim, jid)

	res, err := conn.Exec(ctx, script, nil)
	if err != nil {
		return "", err
	}
	if res.RC != 0 {
		return "", fmt.Errorf("async: launch failed: rc=%d stderr=%s", res.RC, res.Stderr)
	}
	got := strings.TrimSpace(res.Stdout)
	if got != jid {
		return "", fmt.Errorf("async: launch did not confirm the job id (got %q, want %q)", got, jid)
	}
	return jid, nil
}

// validAsyncJID reports whether jid has the shape this package itself
// generates: "<unixnano>.<random>", digits and a single dot. Real
// Ansible's own job ids have that shape too.
//
// ⛔ SECURITY. jid arrives from a playbook argument
// (async_status: {jid: ...}) and used to be interpolated into a shell
// command UNQUOTED, including into `rm -rf`. A jid of
// `x; touch /tmp/pwned ;` ran that touch -- demonstrated by a test in
// this package, which fails if the hole reopens.
//
// shellQuote on every interpolation is what CLOSES the hole; this
// validator is defence in depth behind it.
//
// It must not change what a caller SEES, though. Real accepts a
// malformed jid and reports its ordinary not-found result -- measured:
// `async_status: {jid: nope}` gives msg "could not find job" with
// started/finished true, not an error. A first version of this
// rejected the shape outright and broke that parity, which an existing
// test caught. So a jid that fails this check is treated as NOT FOUND,
// which is both real's answer and a value that never reaches a shell.
func validAsyncJID(jid string) bool {
	if jid == "" || len(jid) > 64 {
		return false
	}
	dots := 0
	for _, r := range jid {
		switch {
		case r >= '0' && r <= '9':
		case r == '.':
			dots++
			if dots > 1 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// AsyncCheck reports a job's current status. found=false means no job
// directory exists at all for jid (a typo, or AsyncCleanup already
// ran) — distinct from done=false, which means the directory exists
// but the job is still running (or, indistinguishably, has not yet
// written its first byte of output — matching real Ansible's own
// async_status in that same ambiguous case). done=true gives rc/
// stdout/stderr, fetched only then (not on every poll, to avoid
// hauling potentially large output over the wire while still waiting).
// AsyncResultsFile resolves the path real reports as `results_file`:
// the job's own file under the async directory. asyncDirExpr is a SHELL
// expression ("$HOME/.ansible_async") rather than a path, so $HOME has
// to be expanded on the TARGET -- reporting the unexpanded form would
// be a different string than real's.
//
// One extra round trip, taken only where the value is actually
// reported, which in practice is async_status's own result.
func AsyncResultsFile(ctx context.Context, conn remoteexec.Connection, jid string) string {
	out, err := run(ctx, conn, "printf %s "+asyncDirExpr+"/"+shellQuote(jid))
	if err != nil {
		return ""
	}
	return out
}

func AsyncCheck(ctx context.Context, conn remoteexec.Connection, jid string) (found, done bool, rc int, stdout, stderr string, err error) {
	// A jid that is not of this package's own shape cannot name a job
	// it created, so it is NOT FOUND -- real's own answer for one.
	if !validAsyncJID(jid) {
		return false, false, 0, "", "", nil
	}
	d := asyncDirExpr + "/" + shellQuote(jid)
	probe := fmt.Sprintf(`d=%s
if [ ! -d "$d" ]; then echo NOTFOUND
elif [ -f "$d/rc" ]; then echo DONE; cat "$d/rc"
else echo RUNNING
fi
`, d)
	res, err := conn.Exec(ctx, probe, nil)
	if err != nil {
		return false, false, 0, "", "", err
	}
	lines := strings.SplitN(res.Stdout, "\n", 2)
	switch strings.TrimSpace(lines[0]) {
	case "NOTFOUND":
		return false, false, 0, "", "", nil
	case "RUNNING":
		return true, false, 0, "", "", nil
	case "DONE":
		rcLine := ""
		if len(lines) > 1 {
			rcLine = strings.TrimSpace(lines[1])
		}
		rc, _ = strconv.Atoi(rcLine)
		out, err := conn.Exec(ctx, fmt.Sprintf(`cat %s/stdout`, d), nil)
		if err != nil {
			return true, true, rc, "", "", err
		}
		errOut, err := conn.Exec(ctx, fmt.Sprintf(`cat %s/stderr`, d), nil)
		if err != nil {
			return true, true, rc, out.Stdout, "", err
		}
		return true, true, rc, out.Stdout, errOut.Stdout, nil
	default:
		return false, false, 0, "", "", fmt.Errorf("async: unexpected status probe output: %q", res.Stdout)
	}
}

// AsyncCleanup removes a job's directory entirely — async_status's
// mode=cleanup.
func AsyncCleanup(ctx context.Context, conn remoteexec.Connection, jid string) error {
	// Nothing to remove for a jid this package never issued, and
	// nothing is sent to a shell either.
	if !validAsyncJID(jid) {
		return nil
	}
	_, err := conn.Exec(ctx, fmt.Sprintf(`rm -rf %s/%s`, asyncDirExpr, shellQuote(jid)), nil)
	return err
}
