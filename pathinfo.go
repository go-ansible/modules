package modules

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// addPathInfo ports AnsibleModule.add_path_info
// (module_utils/basic.py), which real applies to EVERY module result on
// its way out — from _return_formatted, called by both exit_json and
// fail_json:
//
//	path = kwargs.get('path', kwargs.get('dest', None))
//	if path is None: return
//	if os.path.exists(path):
//	    uid gid owner group mode state size
//
// So a result carrying `path` or `dest` that EXISTS gains seven keys,
// whoever produced it, and one that does not exist gains none. It is a
// rule of the framework rather than of any module, which is why it
// belongs here beside finalizeOutput rather than in each module.
//
// Found by reading real's source rather than by measuring a module:
// wait_for's own exit_json names seven keys and real returns thirteen,
// and no amount of staring at wait_for.py explains the other six.
//
// Two consequences worth naming, both measured afterwards:
//
//   - `state` is OVERWRITTEN, not filled in. wait_for asks for
//     `state: started` and real reports `state=file`, because
//     add_path_info replaces it with what the path IS. Nobody would
//     guess that; it is real's behaviour.
//   - the stat is an LSTAT. A symlink reports its OWN size and mode
//     (measured: 25 bytes, 0755) and `state=link`, not its target's.
//     A file with more than one link reports `state=hard`.
func addPathInfo(ctx context.Context, conn remoteexec.Connection, name string, res Result) (Result, error) {
	if conn == nil || res.Extra == nil || controllerSideResult[NormalizeName(name)] {
		return res, nil
	}
	path, ok := pathInfoTarget(res)
	if !ok {
		return res, nil
	}

	info, err := lstatPath(ctx, conn, path)
	if err != nil || info == nil {
		// A path that is not there adds nothing at all, and neither
		// does a stat this port could not run: real's add_path_info
		// simply returns when os.path.exists is false, and never
		// fails a module for it.
		return res, nil
	}

	out := res
	for k, v := range map[string]any{
		"uid":   info.uid,
		"gid":   info.gid,
		"owner": info.owner,
		"group": info.group,
		// Python's '0%03o', which gives 0644 and, for a setuid file,
		// 04755 -- five characters, not four.
		"mode":  fmt.Sprintf("0%03o", info.mode),
		"state": info.state,
		"size":  info.size,
	} {
		out = out.WithExtra(k, v)
	}
	return out, nil
}

// pathInfoTarget is real's own `kwargs.get('path', kwargs.get('dest'))`:
// path first, dest second, neither means nothing to do.
// controllerSideResult names the modules whose result real SYNTHESISES
// on the controller, in an action plugin that never runs a module on
// the target -- so _return_formatted, and with it add_path_info, never
// touches it.
//
// fetch is the one measured: its action plugin does the transfer in
// Python and ends with `return result`, a dict it built itself. Real's
// fetch therefore reports dest WITHOUT uid/gid/owner/group/mode/size/
// state, and the corpus caught this port adding them.
//
// The set is what has been MEASURED, not a taxonomy of real's action
// plugins. Most of those delegate to a module and their results DO get
// path info: copy, file, tempfile, get_url, assemble and ini_file were
// each checked against real and each has it. A new entry here needs
// the same check, not a guess about how real implements it.
var controllerSideResult = map[string]bool{
	"fetch": true,
}

func pathInfoTarget(res Result) (string, bool) {
	for _, key := range []string{"path", "dest"} {
		raw, ok := res.Extra[key]
		if !ok {
			continue
		}
		s, ok := raw.(string)
		if ok && s != "" {
			return s, true
		}
	}
	return "", false
}

// pathInfo is what add_path_info needs, which is statPath's shape plus
// the link COUNT — real reports state=hard for a file with more than
// one, and nothing else in this package needed that.
type pathInfo struct {
	size  int64
	mode  uint32
	uid   int
	gid   int
	owner string
	group string
	state string
}

// lstatPath probes path WITHOUT following a symlink, in GNU stat syntax
// with a BSD fallback. Both are lstat by default -- GNU dereferences
// only under -L, and BSD only under -L too -- which is what real's
// os.lstat does. Returns (nil, nil) when the path does not exist.
//
// Only ONE of the two branches runs on any given machine, so only one
// is under test at a time: a neuter adding -L to the GNU form passes
// on macOS, where GNU stat is absent and the BSD fallback answers. The
// same neuter on the BSD form bites here, and Linux CI covers the GNU
// one the same way round. Said plainly because "the tests pass" means
// less for a two-branch probe than it looks.
func lstatPath(ctx context.Context, conn remoteexec.Connection, path string) (*pathInfo, error) {
	q := shellQuote(path)
	cmd := fmt.Sprintf(
		"stat -c '%%s|%%a|%%F|%%u|%%g|%%U|%%G|%%h' %s 2>/dev/null || stat -f '%%z|%%Lp|%%HT|%%u|%%g|%%Su|%%Sg|%%l' %s 2>/dev/null",
		q, q,
	)
	res, err := conn.Exec(ctx, cmd, nil)
	if err != nil {
		return nil, err
	}
	out := strings.TrimSpace(res.Stdout)
	if res.RC != 0 || out == "" {
		return nil, nil
	}
	parts := strings.SplitN(out, "|", 8)
	if len(parts) < 8 {
		return nil, fmt.Errorf("lstat %s: unexpected output %q", path, out)
	}
	size, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("lstat %s: size %q: %w", path, parts[0], err)
	}
	mode, err := strconv.ParseUint(parts[1], 8, 32)
	if err != nil {
		return nil, fmt.Errorf("lstat %s: mode %q: %w", path, parts[1], err)
	}
	nlink, err := strconv.Atoi(strings.TrimSpace(parts[7]))
	if err != nil {
		nlink = 1
	}
	return &pathInfo{
		size:  size,
		mode:  uint32(mode),
		uid:   atoiOr(parts[3], -1),
		gid:   atoiOr(parts[4], -1),
		owner: parts[5],
		group: parts[6],
		state: pathState(parts[2], nlink),
	}, nil
}

// pathState is real's own ladder, in its order: link, then directory,
// then hard, then file. The order matters -- a symlink whose target has
// several links is still `link`.
func pathState(kind string, nlink int) string {
	switch t := strings.ToLower(kind); {
	case strings.Contains(t, "symbolic link"):
		return "link"
	case strings.Contains(t, "directory"):
		return "directory"
	case nlink > 1:
		return "hard"
	}
	return "file"
}

func atoiOr(s string, def int) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return def
	}
	return n
}
