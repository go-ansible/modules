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
	if current == nil {
		if !create {
			return Fail(fmt.Sprintf("%s does not exist (set create: true to allow creating it)", path)), nil
		}
		current = []byte{}
	}

	lines := splitLines(string(current))
	newLines, changed := applyLineinfile(lines, line, re, state)
	if !changed {
		return Ok(path + " unchanged"), nil
	}

	newContent := strings.Join(newLines, "\n")
	if len(newLines) > 0 {
		newContent += "\n"
	}
	if err := writeRemote(ctx, conn, path, []byte(newContent)); err != nil {
		return Result{}, err
	}
	return Changed(path), nil
}

func applyLineinfile(lines []string, line string, re *pcre.Regexp, state string) ([]string, bool) {
	matches := func(l string) bool {
		if re != nil {
			return re.MatchString(l)
		}
		return l == line
	}

	if state == "absent" {
		var out []string
		removed := false
		for _, l := range lines {
			if matches(l) {
				removed = true
				continue
			}
			out = append(out, l)
		}
		return out, removed
	}

	// state == "present"
	for i, l := range lines {
		if matches(l) {
			if l == line {
				return lines, false
			}
			out := append([]string{}, lines...)
			out[i] = line
			return out, true
		}
	}
	// No matching line: append.
	out := append([]string{}, lines...)
	out = append(out, line)
	return out, true
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
