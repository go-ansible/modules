package modules

import (
	"context"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleGetURL implements (a subset of) Ansible's `get_url` module:
// downloads a URL to a path on the target.
//
// Real get_url runs on the target already (Python was copied there
// before the module ran); this port has no separate module-copy step,
// but the download must still happen on the target rather than the
// control node — downloading here and Put-ing the bytes over would go
// through a network path real get_url never takes, and would silently
// break for a URL only the target can reach. So this composes a remote
// curl/wget invocation via conn.Exec, for the same architectural reason
// documented on moduleURI.
//
// Args: url (string, required); dest (string, required, remote path);
// mode (octal string, optional); force (bool, default false).
//
// Idempotency: real get_url can compare an ETag/Last-Modified header or
// an explicit checksum against the existing destination to decide
// whether to re-download. This port simplifies that to an existence
// check only — it skips the download whenever dest already exists,
// unless force is set. That is weaker than real Ansible (a changed
// remote resource at the same URL is not detected without force),
// which is documented here deliberately.
func moduleGetURL(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	url, err := requireString(args, "url")
	if err != nil {
		return Result{}, err
	}
	dest, err := requireString(args, "dest")
	if err != nil {
		return Result{}, err
	}
	mode, err := argMode(args, "mode")
	if err != nil {
		return Result{}, err
	}
	force := argBool(args, "force", false)

	exists, err := pathExists(ctx, conn, dest)
	if err != nil {
		return Result{}, err
	}

	changed := false
	if !exists || force {
		res, err := runStatus(ctx, conn, getURLCmd(dest, url))
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail(fmt.Sprintf("get_url: downloading %s: %s", url, strings.TrimSpace(res.Stderr))), nil
		}
		changed = true
	}

	if mode != nil {
		info, err := statPath(ctx, conn, dest)
		if err != nil {
			return Result{}, err
		}
		if info == nil || info.mode != *mode {
			if _, err := run(ctx, conn, fmt.Sprintf("chmod %04o %s", *mode, shellQuote(dest))); err != nil {
				return Result{}, err
			}
			changed = true
		}
	}

	r := Ok(dest)
	if changed {
		r = Changed(dest)
	}
	return r.WithExtra("dest", dest).WithExtra("url", url), nil
}

// getURLCmd builds the curl-with-wget-fallback download invocation for
// moduleGetURL, separated out so its exact shape can be asserted
// directly in tests.
func getURLCmd(dest, url string) string {
	destQ := shellQuote(dest)
	urlQ := shellQuote(url)
	return "if command -v curl >/dev/null 2>&1; then curl -fsSL -o " + destQ + " " + urlQ +
		"; elif command -v wget >/dev/null 2>&1; then wget -q -O " + destQ + " " + urlQ +
		"; else echo 'get_url: neither curl nor wget found' >&2; exit 127; fi"
}
