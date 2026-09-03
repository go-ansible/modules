package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleKernelBlacklist implements Ansible's `kernel_blacklist`
// (community.general) module: adds or removes a `blacklist <name>`
// line in a modprobe.d blacklist file.
//
// Args: name (string, required); state (present|absent, default
// "present"); blacklist_file (string, default
// "/etc/modprobe.d/blacklist-ansible.conf").
//
// An existing entry is found by an exact, uncommented "blacklist <name>"
// line — matching sysctl.go's "one line, found by key alone" shape, but
// here the whole line (not a field within it) is the key, and a
// commented-out line is never treated as a match (so re-adding a module
// someone had manually commented out writes a fresh, active line rather
// than uncommenting the old one — real kernel_blacklist's own behavior
// on a pre-existing commented entry was not visible from ansible-doc's
// output and was not independently verified). The file's parent
// directory is created if missing (`mkdir -p`) so a custom
// `blacklist_file` outside the default /etc/modprobe.d doesn't fail on
// a missing directory.
//
// No simplifications of note beyond the above: this module's real
// argspec is small (name, state, blacklist_file) and is implemented in
// full.
func moduleKernelBlacklist(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("kernel_blacklist: state must be present or absent, got %q", state)
	}
	blacklistFile := argString(args, "blacklist_file", "/etc/modprobe.d/blacklist-ansible.conf")

	res, err := runStatus(ctx, conn, "cat "+shellQuote(blacklistFile)+" 2>/dev/null")
	if err != nil {
		return Result{}, err
	}
	var lines []string
	if res.RC == 0 {
		lines = splitLines(res.Stdout)
	}

	newLines, changed := kernelBlacklistApplyEntry(lines, name, state)
	if !changed {
		return Ok(name + " unchanged"), nil
	}

	if dir := kernelBlacklistDirname(blacklistFile); dir != "" {
		if _, err := run(ctx, conn, "mkdir -p "+shellQuote(dir)); err != nil {
			return Result{}, err
		}
	}
	content := strings.Join(newLines, "\n")
	if len(newLines) > 0 {
		content += "\n"
	}
	if err := writeRemote(ctx, conn, blacklistFile, []byte(content)); err != nil {
		return Result{}, err
	}
	return Changed(name), nil
}

func kernelBlacklistApplyEntry(lines []string, name, state string) ([]string, bool) {
	entry := "blacklist " + name
	found := false
	var out []string
	for _, l := range lines {
		if strings.TrimSpace(l) == entry {
			found = true
			if state == "absent" {
				continue
			}
		}
		out = append(out, l)
	}
	if state == "present" && !found {
		out = append(out, entry)
		return out, true
	}
	if state == "absent" && found {
		return out, true
	}
	return lines, false
}

func kernelBlacklistDirname(path string) string {
	i := strings.LastIndexByte(path, '/')
	if i <= 0 {
		return ""
	}
	return path[:i]
}
