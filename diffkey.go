package modules

// The `diff` key real puts in a module's RESULT, which is not the same
// thing as the diff it prints.
//
// This port has a Diffs field that the callback renders under --diff; a
// playbook that registers a result and reads `result.diff` got nothing,
// because the field never reached the result dictionary.
//
// Three shapes, measured against ansible-core 2.21.4 — one per module
// family, so they are built separately rather than from one rule:
//
//	copy        a LIST, empty unless diff is on, and then one entry
//	            whose headers are the bare path
//	lineinfile  a LIST of two: the content, then the file attributes
//	blockinfile (headers only). Present even when diff is off, with
//	            before and after empty.
//	ini_file    a single DICT, not a list, shaped like the content entry
//	replace     ABSENT. It reports rc instead.
//
// before and after are EMPTY unless diff is on. Real fills them only
// then, so a result registered from an ordinary run carries the shape
// and not the content.

// contentDiffEntry is the "<path> (content)" half, which every module
// here but copy uses.
func contentDiffEntry(path, before, after string) map[string]any {
	return map[string]any{
		"before":        before,
		"after":         after,
		"before_header": path + " (content)",
		"after_header":  path + " (content)",
	}
}

// attributesDiffEntry is the second entry lineinfile and blockinfile
// carry: two headers and nothing else, because the attributes did not
// change.
func attributesDiffEntry(path string) map[string]any {
	return map[string]any{
		"before_header": path + " (file attributes)",
		"after_header":  path + " (file attributes)",
	}
}

// fileDiffKey is lineinfile's and blockinfile's whole diff key.
func fileDiffKey(path, before, after string) []any {
	return []any{contentDiffEntry(path, before, after), attributesDiffEntry(path)}
}

// copyDiffKey is copy's: a list that is EMPTY unless diff is on, and
// whose headers are the bare path rather than "<path> (content)".
func copyDiffKey(path, before, after string, wantDiff bool) []any {
	if !wantDiff {
		return []any{}
	}
	return []any{map[string]any{
		"before_header": path,
		"before":        before,
		"after_header":  path,
		"after":         after,
	}}
}

// diffContent returns what belongs in before/after: the real content
// when diff is on, and empty strings otherwise.
func diffContent(on bool, before, after []byte) (string, string) {
	if !on {
		return "", ""
	}
	return string(before), string(after)
}
