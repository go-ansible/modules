package modules

import (
	"context"
	"fmt"
	"os"
	"strings"

	pcre "github.com/go-regexp/engine"
	remoteexec "github.com/go-remoteexec/transport"
)

// moduleLineinfile implements Ansible's `lineinfile` module: ensures a
// particular line is present or absent in a file.
//
// Args: path (required); line (required with state=present); regexp;
// search_string (a SUBSTRING, not a pattern); insertafter and
// insertbefore (a pattern, or the words EOF/BOF); backrefs; firstmatch;
// state (present|absent, default present); create (default false).
// insertbefore|insertafter, regexp|search_string and
// backrefs|search_string are mutually exclusive, as they are in real's
// own argument spec.
//
// Three behaviours here are measured against ansible-core 2.21.4
// rather than inferred, because the reasonable guess is wrong in each:
//
//   - WITHOUT firstmatch, the LAST matching line is the one edited.
//     Real's search loop does not break. On alpha=1/beta=2/alpha=3 with
//     regexp ^alpha=, real rewrites alpha=3.
//   - backrefs with NO match does nothing at all -- not an append. The
//     line cannot be built without the groups that would fill it.
//   - a regexp or search_string match makes insertafter/insertbefore
//     irrelevant; they only place a line that is not there yet.
//
// The error shapes are real's too: rc=257 with "Destination %s does not
// exist !" for a missing file without create, "regexp is required with
// backrefs=true", and state=absent on a missing file reporting
// changed=false with "file not present" rather than failing.
func moduleLineinfile(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	path, err := requireString(args, "path")
	if err != nil {
		return Result{}, err
	}
	spec := lineinfileSpec{
		state:        argString(args, "state", "present"),
		line:         argString(args, "line", ""),
		searchString: argString(args, "search_string", ""),
		insertAfter:  argString(args, "insertafter", ""),
		insertBefore: argString(args, "insertbefore", ""),
		backrefs:     argBool(args, "backrefs", false),
		firstmatch:   argBool(args, "firstmatch", false),
	}
	regexpArg := argString(args, "regexp", "")
	create := argBool(args, "create", false)

	// Real declares these three pairs mutually_exclusive, and the
	// argument spec rejects them before the module body runs.
	for _, pair := range [][2]string{
		{"insertbefore", "insertafter"}, {"regexp", "search_string"}, {"backrefs", "search_string"},
	} {
		if _, a := args[pair[0]]; a {
			if _, b := args[pair[1]]; b {
				return Fail(fmt.Sprintf("parameters are mutually exclusive: %s|%s", pair[0], pair[1])), nil
			}
		}
	}
	if spec.backrefs && regexpArg == "" {
		return Fail("regexp is required with backrefs=true"), nil
	}
	if spec.state == "present" && spec.line == "" {
		if _, ok := args["line"]; !ok {
			return Fail("line is required with state=present"), nil
		}
	}

	var re *pcre.Regexp
	if regexpArg != "" {
		re, err = pcre.Compile(regexpArg)
		if err != nil {
			return Result{}, errArg("lineinfile: invalid regexp: %v", err)
		}
	}
	spec.re = re
	// insertafter/insertbefore are regexes too, except for the two
	// reserved words real treats as positions rather than patterns.
	if spec.insertAfter != "" && spec.insertAfter != "EOF" && spec.insertAfter != "BOF" {
		if spec.insAfterRe, err = pcre.Compile(spec.insertAfter); err != nil {
			return Result{}, errArg("lineinfile: invalid insertafter: %v", err)
		}
	}
	if spec.insertBefore != "" && spec.insertBefore != "BOF" {
		if spec.insBeforeRe, err = pcre.Compile(spec.insertBefore); err != nil {
			return Result{}, errArg("lineinfile: invalid insertbefore: %v", err)
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
		// Measured against ansible-core 2.21.4: state=absent on a
		// missing file is NOT a failure, it is changed=false with this
		// exact message; state=present without create fails with
		// rc=257 and a message whose spacing ("... does not exist !")
		// is real's own.
		if spec.state == "absent" {
			return Result{Extra: map[string]any{"msg": "file not present"}}, nil
		}
		if !create {
			res := Fail(fmt.Sprintf("Destination %s does not exist !", path))
			return res.WithExtra("rc", 257), nil
		}
		current = []byte{}
	}

	lines := splitLines(string(current))
	var newLines []string
	var outcome lineinfileOutcome
	if spec.state == "absent" {
		newLines, outcome = applyLineinfileAbsent(lines, spec)
	} else {
		newLines, outcome = applyLineinfilePresent(lines, spec)
	}
	if !outcome.changed {
		// Real's msg is EMPTY when nothing happened, not a sentence
		// about the file -- and it is PRESENT and empty, not absent,
		// which is why it goes through Extra: a bare Result{} would
		// now omit the key entirely.
		// Real's unchanged lineinfile still carries backup and diff --
		// measured: backup, changed, diff, failed, msg.
		return Result{Extra: map[string]any{
			"msg":    "",
			"backup": "",
			"diff":   fileDiffKey(path, "", ""),
		}}, nil
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
	// The diff RESULT key, and the backup key real reports whether or
	// not a backup was asked for: an empty string when it was not.
	before, after := diffContent(InDiffMode(args), existing, []byte(newContent))
	res = res.WithExtra("diff", fileDiffKey(path, before, after)).WithExtra("backup", "")
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
