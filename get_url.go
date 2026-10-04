package modules

import (
	"context"
	"fmt"
	"strconv"
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

	// sha1 of what is already there, which real reports as
	// checksum_dest -- and the EMPTY string when dest does not exist,
	// measured rather than guessed.
	sumDest := ""
	if exists {
		if v, serr := fileSHA1(ctx, conn, dest); serr == nil {
			sumDest = v
		}
	}

	staged := conn.TempPath("get_url")
	res, err := runStatus(ctx, conn, getURLCmd(staged, dest, url, exists && !force))
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		_ = conn.Remove(ctx, staged)
		return Fail(fmt.Sprintf("get_url: downloading %s: %s", url, strings.TrimSpace(res.Stderr))), nil
	}
	status, elapsed := parseGetURLTrailers(res.Stdout)

	// Nothing downloaded. Two ways to get here, and keying only on the
	// status would miss the second:
	//
	//   - a 304, which is what a conditional request against an
	//     unchanged HTTP resource returns;
	//   - a conditional request curl satisfied without writing at all,
	//     which is what file:// does -- there is no HTTP status there
	//     (%{http_code} is 000), so the staged file's ABSENCE is the
	//     only signal.
	//
	// Real reports fourteen keys in this case, against eighteen for a
	// download: no src, checksum_src, checksum_dest or md5sum.
	staged_exists, serr := pathExists(ctx, conn, staged)
	if serr != nil {
		return Result{}, serr
	}
	if status == 304 || !staged_exists {
		_ = conn.Remove(ctx, staged)
		msg := "HTTP Error 304: Not Modified"
		out := Ok(msg)
		return getURLResultKeys(out, dest, url, "", "", "", "", status, elapsed, false), nil
	}

	size, _ := run(ctx, conn, "wc -c < "+shellQuote(staged))
	sumSrc, _ := fileSHA1(ctx, conn, staged)
	if _, err := run(ctx, conn, "mv "+shellQuote(staged)+" "+shellQuote(dest)); err != nil {
		return Result{}, err
	}
	md5, _ := fileMD5(ctx, conn, dest)
	changed := sumSrc != sumDest

	if mode != nil {
		info, merr := statPath(ctx, conn, dest)
		if merr != nil {
			return Result{}, merr
		}
		if info == nil || info.mode != *mode {
			if _, err := run(ctx, conn, fmt.Sprintf("chmod %04o %s", *mode, shellQuote(dest))); err != nil {
				return Result{}, err
			}
			changed = true
		}
	}

	// Real's msg is the status line's reason phrase plus the body size,
	// the same form uri uses -- "OK (12 bytes)".
	r := Ok(fmt.Sprintf("OK (%s bytes)", strings.TrimSpace(size)))
	if changed {
		r = Changed(fmt.Sprintf("OK (%s bytes)", strings.TrimSpace(size)))
	}
	return getURLResultKeys(r, dest, url, staged, sumSrc, sumDest, md5, status, elapsed, true), nil
}

// parseGetURLTrailers reads the "<status> <seconds>" curl -w writes
// after the body has gone to its own file.
func parseGetURLTrailers(out string) (status int, elapsed float64) {
	f := strings.Fields(strings.TrimSpace(out))
	if len(f) > 0 {
		status, _ = strconv.Atoi(f[0])
	}
	if len(f) > 1 {
		elapsed, _ = strconv.ParseFloat(f[1], 64)
	}
	return status, elapsed
}

// fileSHA1 and fileMD5 ask the target, since the checksums real reports
// are of the file as it exists there.
func fileSHA1(ctx context.Context, conn remoteexec.Connection, path string) (string, error) {
	out, err := run(ctx, conn, "shasum -a 1 "+shellQuote(path)+" 2>/dev/null | awk '{print $1}' || sha1sum "+shellQuote(path)+" | awk '{print $1}'")
	return strings.TrimSpace(out), err
}

func fileMD5(ctx context.Context, conn remoteexec.Connection, path string) (string, error) {
	out, err := run(ctx, conn, "md5 -q "+shellQuote(path)+" 2>/dev/null || md5sum "+shellQuote(path)+" | awk '{print $1}'")
	return strings.TrimSpace(out), err
}

// getURLCmd builds the curl-with-wget-fallback download invocation for
// moduleGetURL, separated out so its exact shape can be asserted
// directly in tests.
// getURLCmd builds the download invocation.
//
// Three things it has to report that the old one-liner could not, each
// measured against ansible-core 2.21.4:
//
//   - the HTTP STATUS, because a second run against an unchanged file
//     is a 304 and real reports changed=false with msg "HTTP Error 304:
//     Not Modified" rather than re-downloading;
//   - the elapsed time, which real reports as whole seconds;
//   - a STAGING path, because real downloads to a temp and moves it,
//     and reports that temp as `src`. Writing straight to dest left
//     `src` with no honest value at all.
//
// -z makes the request CONDITIONAL on the destination's mtime, which is
// what produces the 304 on an unchanged file; force skips it, as real's
// own force does. The wget fallback cannot do any of this, so it stays
// as the unconditional last resort and its caller reports the keys it
// can.
func getURLCmd(staged, dest, url string, conditional bool) string {
	stagedQ, destQ, urlQ := shellQuote(staged), shellQuote(dest), shellQuote(url)
	cond := ""
	if conditional {
		cond = " -z " + destQ
	}
	return "if command -v curl >/dev/null 2>&1; then curl -fsSL" + cond +
		" -o " + stagedQ + " -w " + shellQuote("%{http_code} %{time_total}") + " " + urlQ +
		"; elif command -v wget >/dev/null 2>&1; then wget -q -O " + stagedQ + " " + urlQ +
		" && printf '200 0'" +
		"; else echo 'get_url: neither curl nor wget found' >&2; exit 127; fi"
}

// getURLResultKeys adds the keys real reports. The four
// download-specific ones -- src, checksum_src, checksum_dest, md5sum --
// appear ONLY when a download actually happened: measured, a 304 run
// reports fourteen keys and a 200 run eighteen, differing by exactly
// those four.
func getURLResultKeys(r Result, dest, url, staged, sumSrc, sumDest, md5 string, status int, elapsed float64, downloaded bool) Result {
	r = r.WithExtra("dest", dest).
		WithExtra("url", url).
		WithExtra("status_code", status).
		WithExtra("elapsed", int(elapsed))
	if downloaded {
		r = r.WithExtra("src", staged).
			WithExtra("checksum_src", sumSrc).
			WithExtra("checksum_dest", sumDest).
			WithExtra("md5sum", md5)
	}
	return r
}
