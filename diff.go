package modules

import (
	"bytes"
	"unicode/utf8"
)

// DiffModeKey is the wire flag the engine passes in a task's arguments
// when the run was started with --diff, matching how real Ansible sends
// _ansible_diff alongside the module's own parameters. It travels in
// args for the same reason CheckModeKey does: adding a parameter to
// every module signature to carry one run-wide boolean would change 566
// of them to serve a handful.
const DiffModeKey = "_ansible_diff"

// InDiffMode reports whether args carry the diff flag.
func InDiffMode(args map[string]any) bool {
	v, ok := args[DiffModeKey].(bool)
	return ok && v
}

// MaxDiffSize is real Ansible's MAX_FILE_SIZE_FOR_DIFF default
// (ansible/config/base.yml): a file larger than this is reported as
// skipped rather than diffed, which also bounds the O(n*m) table the
// renderer builds.
const MaxDiffSize int64 = 104448

// Diff is a module's before/after report for a --diff run, the Go form
// of the `diff` key real Ansible modules return. A module fills in only
// what applies: the common case is Before/After plus the two headers,
// naming the file each side came from.
//
// Before and After are the file CONTENTS, not paths. An absent side
// (a file that did not exist) is the empty string, which is what real
// Ansible's own callback normalizes None to.
type Diff struct {
	Before, After string

	// BeforeHeader and AfterHeader name each side in the ---/+++ lines.
	// Real modules put the path there, suffixed with "(content)" when
	// the diff is of contents rather than of metadata — an empty header
	// prints the bare word "before"/"after", which is what real Ansible
	// shows for a file that did not exist yet.
	BeforeHeader, AfterHeader string

	// The skip reasons, which real Ansible reports in place of a diff
	// it declines to compute. A non-zero *Larger is the size limit that
	// was exceeded, and is printed as part of the message.
	SrcBinary, DstBinary bool
	SrcLarger, DstLarger int64

	// Prepared is diff text a module formatted itself, emitted verbatim.
	// Real Ansible's `prepared` key; used by modules whose change is not
	// a file's contents.
	Prepared string
}

// Empty reports whether d carries nothing worth showing.
func (d Diff) Empty() bool {
	return d == Diff{}
}

// ContentDiff builds a Diff for a file whose contents are changing,
// applying real Ansible's own binary and size checks to both sides.
// A nil before means the file did not exist yet.
//
// Both sides are passed in rather than read here, because a module
// reaches its target through a Connection: the before content comes
// from the target, which may not be this machine.
//
// The headers are the caller's to choose because real Ansible has NO
// single convention for them — each module names its sides its own way,
// measured against ansible-core 2.21.4:
//
//	lineinfile, blockinfile  "<path> (content)" on both sides
//	replace                  the bare path on both sides
//	copy with content:       the bare dest path on both sides
//	copy with src:           dest before, the SOURCE path after
//
// A nil before side overrides beforeHeader and leaves it empty, which is
// what makes real Ansible print a bare "--- before" for a file that did
// not exist: naming it would claim there were contents to compare.
func ContentDiff(beforeHeader, afterHeader string, before, after []byte) Diff {
	d := Diff{AfterHeader: afterHeader}

	if before != nil {
		d.BeforeHeader = beforeHeader
		switch {
		case int64(len(before)) > MaxDiffSize:
			d.SrcLarger = MaxDiffSize
		case isBinary(before):
			d.SrcBinary = true
		default:
			d.Before = string(before)
		}
	}

	switch {
	case int64(len(after)) > MaxDiffSize:
		d.DstLarger = MaxDiffSize
	case isBinary(after):
		d.DstBinary = true
	default:
		d.After = string(after)
	}
	return d
}

// ContentHeader is the "<path> (content)" form lineinfile and
// blockinfile use for both of their headers.
func ContentHeader(path string) string { return path + " (content)" }

// isBinary is real Ansible's own test for content it will not diff: a
// NUL byte anywhere, or bytes that are not valid text at all. Real
// Ansible checks for b"\x00" in the first chunk; the UTF-8 check is
// added here because this port holds contents as Go strings, where
// invalid bytes would otherwise render as replacement characters in the
// diff rather than being declined.
func isBinary(b []byte) bool {
	return bytes.IndexByte(b, 0) >= 0 || !utf8.Valid(b)
}
