package modules

import (
	"context"
	"fmt"
	"strings"

	pcre "github.com/go-regexp/engine"
	remoteexec "github.com/go-remoteexec/transport"
)

// moduleBlockinfile implements (a subset of) Ansible's `blockinfile`
// module: ensures a marked, multi-line block of text is present in (or
// absent from) a file, wrapped between two marker-comment lines so a
// later run can find and replace exactly the block it wrote.
//
// Args: path (string, required); block (string, default "" — an empty
// block is treated as state=absent regardless of the state argument,
// per this batch's task spec; real ansible.builtin.blockinfile instead
// inserts an empty marked block in that case, so this is a deliberate
// deviation, documented here); marker (string, default "# {mark}
// ANSIBLE MANAGED BLOCK" — "{mark}" is replaced with "BEGIN"/"END" to
// form the two marker lines); state (present|absent, default
// "present"); insertafter/insertbefore (string, optional — a regexp
// matched against existing lines; the block is inserted after/before
// the first match, or at EOF if neither is given or the regexp doesn't
// match; the literal values "BOF"/"EOF" are also accepted, matching
// real blockinfile's own special-cased anchors); create (bool, default
// false — create the file if it doesn't exist).
//
// If a marked block already exists, it is replaced in place at its
// existing location, ignoring insertafter/insertbefore for that run —
// insertafter/insertbefore only decide where a new block is inserted
// the first time, matching real blockinfile's own behavior.
//
// Simplifications vs real blockinfile: no backup, owner/group/mode/
// attributes, SELinux context, validate, encoding, or
// append_newline/prepend_newline support. insertafter/insertbefore are
// full regexps (via the same engine lineinfile/replace use), not
// Python's re — differences are expected to be rare for the anchor
// patterns this module is typically given.
func moduleBlockinfile(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	path, err := requireString(args, "path")
	if err != nil {
		return Result{}, err
	}
	block := argString(args, "block", "")
	marker := argString(args, "marker", "# {mark} ANSIBLE MANAGED BLOCK")
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("blockinfile: state must be present or absent, got %q", state)
	}
	insertafter := argString(args, "insertafter", "")
	insertbefore := argString(args, "insertbefore", "")
	create := argBool(args, "create", false)

	effectiveState := state
	if block == "" {
		effectiveState = "absent"
	}

	current, err := fetchIfExists(ctx, conn, path)
	if err != nil {
		return Result{}, err
	}
	if current == nil {
		if effectiveState == "absent" {
			return Ok(path + " unchanged (does not exist)"), nil
		}
		if !create {
			return Fail(fmt.Sprintf("%s does not exist (set create: true to allow creating it)", path)), nil
		}
		current = []byte{}
	}

	markerBegin := strings.Replace(marker, "{mark}", "BEGIN", 1)
	markerEnd := strings.Replace(marker, "{mark}", "END", 1)

	lines := splitLines(string(current))
	beginIdx, endIdx := findMarkerBlock(lines, markerBegin, markerEnd)
	existed := beginIdx >= 0

	var without []string
	if existed {
		without = append(append([]string{}, lines[:beginIdx]...), lines[endIdx+1:]...)
	} else {
		without = lines
	}

	var result []string
	if effectiveState == "absent" {
		result = without
	} else {
		blockLines := append([]string{markerBegin}, append(splitLines(block), markerEnd)...)
		insertIdx := len(without)
		if existed {
			insertIdx = beginIdx
		} else {
			insertIdx, err = blockInsertIndex(without, insertafter, insertbefore)
			if err != nil {
				return Result{}, err
			}
		}
		result = append(append(append([]string{}, without[:insertIdx]...), blockLines...), without[insertIdx:]...)
	}

	newContent := strings.Join(result, "\n")
	if len(result) > 0 {
		newContent += "\n"
	}
	if newContent == string(current) {
		return Ok(path + " unchanged"), nil
	}
	if err := writeRemote(ctx, conn, path, []byte(newContent)); err != nil {
		return Result{}, err
	}
	if effectiveState == "absent" {
		return Changed(path + ": removed block"), nil
	}
	return Changed(path + ": inserted/updated block"), nil
}

// findMarkerBlock locates an existing marked block in lines, returning
// the indices of its begin/end marker lines (both -1 if no exact-match
// pair is found, or if an end marker never follows a begin marker).
func findMarkerBlock(lines []string, markerBegin, markerEnd string) (beginIdx, endIdx int) {
	beginIdx, endIdx = -1, -1
	for i, l := range lines {
		if l == markerBegin {
			beginIdx = i
			for j := i + 1; j < len(lines); j++ {
				if lines[j] == markerEnd {
					endIdx = j
					return beginIdx, endIdx
				}
			}
			return -1, -1 // begin with no matching end: treat as absent
		}
	}
	return -1, -1
}

// blockInsertIndex resolves insertafter/insertbefore into an insertion
// index into lines, for a block being inserted for the first time (see
// moduleBlockinfile's doc comment for the precedence/fallback rules).
func blockInsertIndex(lines []string, insertafter, insertbefore string) (int, error) {
	switch {
	case insertbefore == "BOF":
		return 0, nil
	case insertafter != "" && insertafter != "EOF":
		re, err := pcre.Compile(insertafter)
		if err != nil {
			return 0, errArg("blockinfile: invalid insertafter regexp: %v", err)
		}
		for i, l := range lines {
			if re.MatchString(l) {
				return i + 1, nil
			}
		}
		return len(lines), nil
	case insertbefore != "" && insertbefore != "EOF":
		re, err := pcre.Compile(insertbefore)
		if err != nil {
			return 0, errArg("blockinfile: invalid insertbefore regexp: %v", err)
		}
		for i, l := range lines {
			if re.MatchString(l) {
				return i, nil
			}
		}
		return len(lines), nil
	default:
		return len(lines), nil // EOF, or neither given
	}
}
