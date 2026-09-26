package modules

import (
	"context"
	"fmt"
	"os"
	"strings"

	pcre "github.com/go-regexp/engine"
	remoteexec "github.com/go-remoteexec/transport"
)

// moduleLineinfile implements (a subset of) Ansible's `lineinfile`
// module: ensures a particular line is present or absent in a file.
//
// Args: path (string, required); line (string, required unless
// state=absent with regexp); regexp (string) — when set, the line
// replacing/removed is whichever existing line matches it, otherwise an
// exact-line match is used; state (present|absent, default "present");
// create (bool, default false) — create the file if it doesn't exist.
func moduleLineinfile(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	path, err := requireString(args, "path")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	line := argString(args, "line", "")
	regexpArg := argString(args, "regexp", "")
	create := argBool(args, "create", false)

	var re *pcre.Regexp
	if regexpArg != "" {
		re, err = pcre.Compile(regexpArg)
		if err != nil {
			return Result{}, errArg("lineinfile: invalid regexp: %v", err)
		}
	}

	current, err := fetchIfExists(ctx, conn, path)
	if err != nil {
		return Result{}, err
	}
	// Captured before the nil is replaced below: a file being created
	// has no before side at all, which is what prints a bare
	// "--- before" rather than claiming empty contents to compare.
	existing := current
	if current == nil {
		if !create {
			return Fail(fmt.Sprintf("%s does not exist (set create: true to allow creating it)", path)), nil
		}
		current = []byte{}
	}

	lines := splitLines(string(current))
	newLines, outcome := applyLineinfile(lines, line, re, state)
	if !outcome.changed {
		// Real's msg is EMPTY when nothing happened, not a sentence
		// about the file -- and it is PRESENT and empty, not absent,
		// which is why it goes through Extra: a bare Result{} would
		// now omit the key entirely.
		return Result{Extra: map[string]any{"msg": ""}}, nil
	}

	newContent := strings.Join(newLines, "\n")
	if len(newLines) > 0 {
		newContent += "\n"
	}
	// Every "unchanged" case has already returned above, so reaching here
	// means the file WOULD be rewritten. Check mode reports that and
	// stops short of the one write.
	res := Changed(outcome.msg)
	if outcome.removed > 0 {
		res.Extra = map[string]any{"found": outcome.removed}
	}
	if InDiffMode(args) {
		res = res.WithDiff(ContentDiff(ContentHeader(path), ContentHeader(path), existing, []byte(newContent)))
	}
	if InCheckMode(args) {
		return res, nil
	}
	if err := writeRemote(ctx, conn, path, []byte(newContent)); err != nil {
		return Result{}, err
	}
	return res, nil
}

// lineinfileOutcome is what the edit DID, because real reports it as
// the result's msg -- "line added", "line replaced", "N line(s)
// removed" -- rather than naming the file. A playbook that shows
// r.msg was showing a path here.
type lineinfileOutcome struct {
	changed bool
	msg     string
	// removed is reported as "found" alongside the message, which is
	// how a playbook learns how many lines the pattern matched.
	removed int
}

func applyLineinfile(lines []string, line string, re *pcre.Regexp, state string) ([]string, lineinfileOutcome) {
	matches := func(l string) bool {
		if re != nil {
			return re.MatchString(l)
		}
		return l == line
	}

	if state == "absent" {
		var out []string
		removed := 0
		for _, l := range lines {
			if matches(l) {
				removed++
				continue
			}
			out = append(out, l)
		}
		if removed == 0 {
			return out, lineinfileOutcome{}
		}
		return out, lineinfileOutcome{
			changed: true,
			msg:     fmt.Sprintf("%d line(s) removed", removed),
			removed: removed,
		}
	}

	// state == "present"
	for i, l := range lines {
		if matches(l) {
			if l == line {
				return lines, lineinfileOutcome{}
			}
			out := append([]string{}, lines...)
			out[i] = line
			return out, lineinfileOutcome{changed: true, msg: "line replaced"}
		}
	}
	// No matching line: append.
	out := append([]string{}, lines...)
	out = append(out, line)
	return out, lineinfileOutcome{changed: true, msg: "line added"}
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func writeRemote(ctx context.Context, conn remoteexec.Connection, path string, content []byte) error {
	tmp, err := os.CreateTemp("", "go-ansible-write-*")
	if err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		return fmt.Errorf("writing %s: %w", path, err)
	}
	tmp.Close()
	if err := conn.Put(ctx, tmpPath, path, remoteexec.PutOptions{MkdirParents: true}); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}
