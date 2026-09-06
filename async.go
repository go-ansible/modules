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

// AsyncCheck reports a job's current status. found=false means no job
// directory exists at all for jid (a typo, or AsyncCleanup already
// ran) — distinct from done=false, which means the directory exists
// but the job is still running (or, indistinguishably, has not yet
// written its first byte of output — matching real Ansible's own
// async_status in that same ambiguous case). done=true gives rc/
// stdout/stderr, fetched only then (not on every poll, to avoid
// hauling potentially large output over the wire while still waiting).
func AsyncCheck(ctx context.Context, conn remoteexec.Connection, jid string) (found, done bool, rc int, stdout, stderr string, err error) {
	d := asyncDirExpr + "/" + jid
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
	_, err := conn.Exec(ctx, fmt.Sprintf(`rm -rf %s/%s`, asyncDirExpr, jid), nil)
	return err
}
