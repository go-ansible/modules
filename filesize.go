package modules

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleFilesize implements (a subset of) Ansible's `filesize` module:
// ensures a regular file exists at exactly a given size, creating it if
// missing, growing it (appending bytes from `source`, default
// /dev/zero, without touching existing bytes) or truncating it (cutting
// off bytes without touching what remains) as needed.
//
// Args: path (string, required); size (string, required) — a number
// optionally followed by an SI (KB/MB/.../1000-based) or IEC
// (K/KiB/M/MiB/.../1024-based) multiplicative suffix; with no suffix the
// number is a count of `blocksize`-byte blocks, matching real filesize's
// own documented parsing exactly; blocksize (string, optional) — bytes,
// with the same suffix grammar as size but never block-relative itself;
// defaults to 512 when unset (real filesize queries the OS block size
// here; this port hardcodes a plausible constant instead — documented
// deviation); source (string, default "/dev/zero"); sparse (bool,
// default false); force (bool, default false) — mutually exclusive with
// sparse, matching real filesize; mode (octal string, optional).
//
// Growth is implemented via `dd` (real filesize is itself documented as
// "a simple wrapper around dd"); shrink and any sparse resize (grow or
// shrink) is implemented via `truncate -s`, since GNU/BSD truncate's own
// documented behavior of leaving a sparse hole when growing past EOF is
// a more direct match for `sparse=true` than composing dd calls, and its
// plain byte-offset semantics make shrinking exact regardless of
// blocksize.
//
// Simplifications vs real filesize: no attributes/owner/group/
// selinux(se*)/unsafe_writes. Real filesize picks the largest blocksize
// that evenly divides both the current and target size, to minimize the
// number of dd operations; this port instead always attempts the
// resolved blocksize first and falls back to a single bs=1 dd call
// whenever the current size or the size delta isn't an exact multiple of
// it — correct in all cases, but potentially far slower than real
// filesize's own arithmetic for a large, deliberately-unaligned resize.
func moduleFilesize(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	path, err := requireString(args, "path")
	if err != nil {
		return Result{}, err
	}
	sizeArg, err := requireString(args, "size")
	if err != nil {
		return Result{}, err
	}
	sparse := argBool(args, "sparse", false)
	force := argBool(args, "force", false)
	if sparse && force {
		return Result{}, errArg("filesize: sparse and force are mutually exclusive")
	}
	source := argString(args, "source", "/dev/zero")
	mode, err := argMode(args, "mode")
	if err != nil {
		return Result{}, err
	}

	blocksizeArg := argString(args, "blocksize", "")
	var blocksize int64 = 512
	if blocksizeArg != "" {
		bs, err := filesizeParseBytes(blocksizeArg, 1)
		if err != nil {
			return Result{}, errArg("filesize: blocksize: %v", err)
		}
		blocksize = bs
	}
	target, err := filesizeParseBytes(sizeArg, blocksize)
	if err != nil {
		return Result{}, errArg("filesize: size: %v", err)
	}
	if target < 0 {
		return Result{}, errArg("filesize: size must not be negative")
	}

	info, err := statPath(ctx, conn, path)
	if err != nil {
		return Result{}, err
	}
	var current int64
	if info != nil {
		current = info.size
	}

	changed := false
	var cmd string
	switch {
	case force:
		cmd = filesizeGrowCmd(source, path, blocksize, 0, target)
		if _, err := run(ctx, conn, "truncate -s 0 "+shellQuote(path)+" 2>/dev/null || : > "+shellQuote(path)); err != nil {
			return Result{}, err
		}
		if target > 0 {
			if _, err := run(ctx, conn, cmd); err != nil {
				return Result{}, err
			}
		}
		changed = true

	case target == current:
		// no-op

	case target > current:
		if sparse {
			cmd = "truncate -s " + strconv.FormatInt(target, 10) + " " + shellQuote(path)
		} else {
			cmd = filesizeGrowCmd(source, path, blocksize, current, target)
		}
		if _, err := run(ctx, conn, cmd); err != nil {
			return Result{}, err
		}
		changed = true

	default: // target < current
		cmd = "truncate -s " + strconv.FormatInt(target, 10) + " " + shellQuote(path)
		if _, err := run(ctx, conn, cmd); err != nil {
			return Result{}, err
		}
		changed = true
	}

	if mode != nil {
		info, err := statPath(ctx, conn, path)
		if err != nil {
			return Result{}, err
		}
		if info == nil || info.mode != *mode {
			if _, err := run(ctx, conn, fmt.Sprintf("chmod %04o %s", *mode, shellQuote(path))); err != nil {
				return Result{}, err
			}
			changed = true
		}
	}

	r := Ok(path)
	if changed {
		r = Changed(path)
	}
	r = r.WithExtra("path", path).WithExtra("size_diff", target-current)
	r = r.WithExtra("filesize", map[string]any{
		"bytes":     target,
		"blocksize": blocksize,
		"blocks":    filesizeBlocks(target, blocksize),
		"iec":       filesizeHumanIEC(target),
		"si":        filesizeHumanSI(target),
	})
	if changed && cmd != "" {
		r = r.WithExtra("cmd", cmd)
	}
	return r, nil
}

func filesizeBlocks(bytes, blocksize int64) int64 {
	if blocksize <= 0 || bytes%blocksize != 0 {
		return 0
	}
	return bytes / blocksize
}

// filesizeGrowCmd builds a dd invocation appending (target-current)
// bytes from source to path, without disturbing existing bytes
// (conv=notrunc plus a seek to the current size). It uses blocksize as
// the dd block size when both current and the delta are exact multiples
// of it, falling back to bs=1 (see moduleFilesize's doc comment).
func filesizeGrowCmd(source, path string, blocksize, current, target int64) string {
	delta := target - current
	bs := int64(1)
	if blocksize > 0 && current%blocksize == 0 && delta%blocksize == 0 {
		bs = blocksize
	}
	seek := current / bs
	count := delta / bs
	return fmt.Sprintf("dd if=%s of=%s bs=%d seek=%d count=%d conv=notrunc 2>/dev/null",
		shellQuote(source), shellQuote(path), bs, seek, count)
}

// filesizeParseBytes parses a filesize/blocksize value per the
// grammar documented on moduleFilesize: a number, optionally followed
// by an SI (1000-based) or IEC (1024-based) multiplicative suffix; with
// no suffix, blockRelative is multiplied in as the value's unit (pass 1
// for a plain byte count, as blocksize itself uses).
func filesizeParseBytes(s string, blockRelative int64) (int64, error) {
	s = strings.TrimSpace(s)
	i := 0
	for i < len(s) && (s[i] == '.' || s[i] == '-' || s[i] == '+' || (s[i] >= '0' && s[i] <= '9')) {
		i++
	}
	if i == 0 {
		return 0, fmt.Errorf("invalid size %q", s)
	}
	numPart, suffix := s[:i], strings.TrimSpace(s[i:])
	value, err := strconv.ParseFloat(numPart, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size %q: %w", s, err)
	}

	if suffix == "" {
		return int64(math.Round(value)) * blockRelative, nil
	}
	factor, err := filesizeUnitFactor(suffix)
	if err != nil {
		return 0, fmt.Errorf("invalid size %q: %w", s, err)
	}
	return int64(math.Round(value * factor)), nil
}

// filesizeUnitFactor resolves a multiplicative suffix (e.g. "K", "KiB",
// "KB", "MB", "GiB") to its byte factor: a bare letter or an explicit
// "iB" suffix is IEC/1024-based; a "B" suffix (with no "i") is
// SI/1000-based, matching the grammar in real filesize's own `size` doc.
func filesizeUnitFactor(suffix string) (float64, error) {
	if strings.EqualFold(suffix, "B") {
		return 1, nil
	}
	upper := strings.ToUpper(suffix)
	var letter byte
	iec := false
	switch {
	case strings.HasSuffix(upper, "IB"):
		letter = upper[0]
		iec = true
	case strings.HasSuffix(upper, "B"):
		letter = upper[0]
		iec = false
	case len(upper) == 1:
		letter = upper[0]
		iec = true
	default:
		return 0, fmt.Errorf("unrecognized unit %q", suffix)
	}
	power := strings.IndexByte("_KMGTPEZY", letter)
	if power <= 0 {
		return 0, fmt.Errorf("unrecognized unit %q", suffix)
	}
	if iec {
		return math.Pow(1024, float64(power)), nil
	}
	return math.Pow(1000, float64(power)), nil
}

func filesizeHumanSI(bytes int64) string {
	return filesizeHuman(bytes, 1000.0, "", "kB", "MB", "GB", "TB", "PB", "EB", "ZB", "YB")
}

func filesizeHumanIEC(bytes int64) string {
	return filesizeHuman(bytes, 1024.0, "", "KiB", "MiB", "GiB", "TiB", "PiB", "EiB", "ZiB", "YiB")
}

func filesizeHuman(bytes int64, base float64, units ...string) string {
	v := float64(bytes)
	i := 0
	for v >= base && i < len(units)-1 {
		v /= base
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%d B", bytes)
	}
	return fmt.Sprintf("%.1f %s", v, units[i])
}
