package modules

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// The POSIX stat dictionary real Ansible reports, which `stat` returns
// under "stat" and `find` returns per file. This port used to report
// seven keys where real reports forty-seven, so a playbook branching on
// stat.pw_name, stat.mtime or stat.checksum got nothing at all — and
// the existing corpus case passed because it compared a few fields and
// never the shape.
//
// Every key and type below was measured from real ansible-core 2.21.4 on
// a regular file, a directory and a symlink. The ones worth naming:
// mode is a STRING of four octal digits, the four timestamps are FLOATS
// with sub-second precision, checksum is sha1, and mimetype/charset come
// from file(1).
//
// statProbe gathers all of it in one round trip, in the shape
// "key|value" per line, so the mapping can be tested against measured
// values without a connection.
const statProbeScript = `
p() { printf '%s|%s\n' "$1" "$2"; }
path=__PATH__
__FOLLOWTEST__ || { p exists false; exit 0; }
p exists true
# BSD (macOS, the BSDs) first, then GNU. The two disagree on every
# letter, so the whole line is tried one way and then the other rather
# than field by field.
if line=$(stat __BSDFLAG__ -f '%z|%Lp|%HT|%u|%g|%Su|%Sg|%i|%d|%l|%b|%k|%r|%f' "$path" 2>/dev/null); then
  p fields "$line"
  # %F prefixes the fractional form; without it BSD stat rounds to the
  # second, and real reports floats.
  p times "$(stat __BSDFLAG__ -f '%Fa|%Fm|%Fc|%FB' "$path" 2>/dev/null)"
elif line=$(stat __GNUFLAG__ -c '%s|%a|%F|%u|%g|%U|%G|%i|%d|%h|%b|%B|%t|0' "$path" 2>/dev/null); then
  p fields "$line"
  # GNU reports %b in 512-byte units already and %B as the unit size;
  # the block_size real reports is the filesystem's, %o.
  p gnu_blocksize "$(stat __GNUFLAG__ -c '%o' "$path" 2>/dev/null)"
  p times "$(stat __GNUFLAG__ -c '%.9X|%.9Y|%.9Z|%.9W' "$path" 2>/dev/null)"
fi
[ -r "$path" ] && p readable true || p readable false
[ -w "$path" ] && p writeable true || p writeable false
[ -x "$path" ] && p executable true || p executable false
if [ -L "$path" ]; then
  t=$(readlink "$path" 2>/dev/null); p lnk_target "$t"
  # lnk_source is the FULLY RESOLVED target -- measured: real reports
  # /private/tmp/... where lnk_target is /tmp/..., so it resolves every
  # component and not just the last one.
  p lnk_source "$(cd "$(dirname "$t")" 2>/dev/null && printf '%s/%s\n' "$(pwd -P)" "$(basename "$t")")"
fi
__CHECKSUM__
__MIME__
`

// statOptions mirrors the arguments of real's stat that change which
// keys come back at all.
type statOptions struct {
	Follow      bool // stat(2) rather than lstat(2); real defaults to FALSE
	GetChecksum bool // real defaults to true
	GetMime     bool // real defaults to true
}

// statDict runs the probe and maps it to real's dictionary. A path that
// does not exist yields the two keys real reports for one, and nothing
// else.
func statDict(ctx context.Context, conn remoteexec.Connection, path string, opts statOptions) (map[string]any, error) {
	script := strings.NewReplacer(
		"__PATH__", shellQuote(path),
		// -e is false for a dangling symlink, which still EXISTS as far
		// as lstat and real are concerned; -L covers that.
		"__FOLLOWTEST__", map[bool]string{true: `[ -e "$path" ]`, false: `[ -e "$path" ] || [ -L "$path" ]`}[opts.Follow],
		"__BSDFLAG__", map[bool]string{true: "-L", false: ""}[opts.Follow],
		"__GNUFLAG__", map[bool]string{true: "-L", false: ""}[opts.Follow],
		"__CHECKSUM__", checksumProbe(opts.GetChecksum),
		"__MIME__", mimeProbe(opts.GetMime),
	).Replace(statProbeScript)

	res, err := conn.Exec(ctx, script, nil)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", path, err)
	}
	return assembleStat(path, parseStatProbe(res.Stdout)), nil
}

func checksumProbe(want bool) string {
	if !want {
		return ""
	}
	// Real reports sha1. sha1sum is GNU, shasum is the BSD/macOS
	// spelling; a directory has no checksum and real reports the empty
	// string for one.
	return `if [ -f "$path" ]; then
  p checksum "$( (sha1sum "$path" 2>/dev/null || shasum -a 1 "$path" 2>/dev/null) | awk '{print $1}')"
fi`
}

func mimeProbe(want bool) string {
	if !want {
		return ""
	}
	return `p mimetype "$(file --mime-type -b "$path" 2>/dev/null)"
p charset "$(file --mime-encoding -b "$path" 2>/dev/null)"`
}

// parseStatProbe turns the probe's "key|value" lines into a map. The
// value may itself contain '|' (the packed field lines do), so only the
// FIRST separator is a separator.
func parseStatProbe(out string) map[string]string {
	raw := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		k, v, ok := strings.Cut(strings.TrimRight(line, "\r"), "|")
		if !ok || k == "" {
			continue
		}
		raw[k] = v
	}
	return raw
}

// assembleStat maps the probe's answers onto real's dictionary. It is
// separate from statDict so the whole shape can be checked against
// values measured from real without running a probe — which is most of
// what there is to get wrong here.
func assembleStat(path string, raw map[string]string) map[string]any {
	if raw["exists"] != "true" {
		// MEASURED, after I first wrote this comment claiming two keys
		// without having checked: real reports exactly ONE key for a
		// missing path, exists, and not even the path it was asked
		// about.
		return map[string]any{"exists": false}
	}

	// %z|%Lp|%HT|%u|%g|%Su|%Sg|%i|%d|%l|%b|%k|%r|%f
	f := strings.Split(raw["fields"], "|")
	get := func(i int) string {
		if i < len(f) {
			return f[i]
		}
		return ""
	}
	mode := parseOctal(get(1))
	kind := statKind(get(2))

	out := map[string]any{
		"exists": true,
		"path":   path,
		"size":   parseInt(get(0)),
		// A STRING of four octal digits, not a number.
		"mode":    fmt.Sprintf("%04o", mode),
		"uid":     parseInt(get(3)),
		"gid":     parseInt(get(4)),
		"pw_name": get(5),
		"gr_name": get(6),
		"inode":   parseInt(get(7)),
		"dev":     parseInt(get(8)),
		"nlink":   parseInt(get(9)),
		"blocks":  parseInt(get(10)),
		// The filesystem's block size. GNU reports it separately,
		// because its %B is the unit %b counts in rather than this.
		"block_size":  firstInt(raw["gnu_blocksize"], get(11)),
		"device_type": parseInt(get(12)),
		"flags":       parseInt(get(13)),
		// Real computes this from the 512-byte block count, so a sparse
		// file reports less than its size.
		"disk_usage_bytes": parseInt(get(10)) * 512,

		"isreg":  kind == "regular",
		"isdir":  kind == "directory",
		"islnk":  kind == "symlink",
		"isblk":  kind == "block",
		"ischr":  kind == "char",
		"isfifo": kind == "fifo",
		"issock": kind == "socket",
		"isuid":  mode&0o4000 != 0,
		"isgid":  mode&0o2000 != 0,

		"rusr": mode&0o400 != 0,
		"wusr": mode&0o200 != 0,
		"xusr": mode&0o100 != 0,
		"rgrp": mode&0o040 != 0,
		"wgrp": mode&0o020 != 0,
		"xgrp": mode&0o010 != 0,
		"roth": mode&0o004 != 0,
		"woth": mode&0o002 != 0,
		"xoth": mode&0o001 != 0,

		// Whether the user running this can read/write/execute it, which
		// is an access(2) question and not a mode question: root reads a
		// 0000 file.
		"readable":   raw["readable"] == "true",
		"writeable":  raw["writeable"] == "true",
		"executable": raw["executable"] == "true",

		// Linux-only in real, and empty or zero everywhere this port was
		// measured. Reported so the keys exist -- a playbook reading
		// stat.attributes should get a list rather than an undefined --
		// but NOT populated: lsattr output is not ported, and inventing
		// values would be worse than empty ones.
		"attributes": []any{},
		"attr_flags": "",
		"version":    nil,
		"generation": 0,
	}

	// atime|mtime|ctime|birthtime, as floats.
	times := strings.Split(raw["times"], "|")
	for i, key := range []string{"atime", "mtime", "ctime", "birthtime"} {
		if i < len(times) {
			out[key] = parseFloat(times[i])
		}
	}

	for _, key := range []string{"checksum", "mimetype", "charset", "lnk_target", "lnk_source"} {
		if v, ok := raw[key]; ok {
			out[key] = v
		}
	}
	return out
}

// statKind normalises the file type word, which BSD stat spells
// "Regular File" and GNU spells "regular file".
func statKind(t string) string {
	switch t = strings.ToLower(t); {
	case strings.Contains(t, "directory"):
		return "directory"
	case strings.Contains(t, "symbolic link"):
		return "symlink"
	case strings.Contains(t, "block"):
		return "block"
	case strings.Contains(t, "character"):
		return "char"
	case strings.Contains(t, "fifo"):
		return "fifo"
	case strings.Contains(t, "socket"):
		return "socket"
	case strings.Contains(t, "regular"):
		return "regular"
	}
	return "other"
}

func parseInt(s string) int64 {
	n, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	return n
}

func parseOctal(s string) uint32 {
	n, _ := strconv.ParseUint(strings.TrimSpace(s), 8, 32)
	return uint32(n)
}

func parseFloat(s string) float64 {
	n, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return n
}

// firstInt takes the first of the two that parses to something non-zero,
// so a field only one of the two stat flavours reports still lands.
func firstInt(preferred, fallback string) int64 {
	if n := parseInt(preferred); n != 0 {
		return n
	}
	return parseInt(fallback)
}
