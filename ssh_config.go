package modules

import (
	"context"
	"sort"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleSSHConfig implements (a subset of) Ansible's `ssh_config` module:
// ensures a `Host <alias>` block is present in (or absent from) an
// OpenSSH client config file.
//
// Unlike known_hosts.go's flat, marker-free line management, ssh_config
// is structurally block-based: a `Host <alias>` line followed by
// indented option lines, running until the next top-level `Host` line
// or EOF. This module borrows blockinfile.go's find/replace-in-place-
// or-insert shape for that reason, but the block is delimited by its
// own `Host <alias>` header line rather than blockinfile's pair of
// marker comments — there is no separate begin/end marker to look for,
// and the block's own first line doubles as both its identity and its
// start boundary.
//
// Args: host (string, required) — the alias/pattern this block is
// filed under; state (present|absent, default "present"); user
// (string) — manage `~<user>/.ssh/config` (mutually exclusive with
// ssh_config_file); ssh_config_file (string, path) — manage this exact
// path instead (mutually exclusive with user); if neither is given,
// "/etc/ssh/ssh_config" is used, matching real ssh_config's own
// documented default. Per-host options, each written as one
// `OptionName value` line inside the block when non-empty/given:
// hostname (-> HostName), port (-> Port, a string per real ssh_config's
// own argspec, not an int), remote_user (-> User), identity_file (->
// IdentityFile), proxycommand (-> ProxyCommand) and proxyjump (->
// ProxyJump, mutually exclusive with proxycommand — validated),
// forward_agent/add_keys_to_agent/identities_only (bool -> "yes"/"no",
// only emitted when the arg key is present at all, since their real
// default is "unset", not false), controlmaster, controlpath,
// controlpersist, dynamicforward, address_family, host_key_algorithms,
// strict_host_key_checking, user_known_hosts_file (plain string
// options), and other_options (a dict of arbitrary lower-case option
// names to string values, each emitted as its own line, in sorted key
// order for deterministic, idempotent output — real ssh_config
// validates the same "keys must be lower case, values must be
// strings" constraints this port enforces).
//
// If a `Host <host>` block already exists, it is replaced in place at
// its existing position in the file; otherwise a new block is appended
// at EOF (real ssh_config has no insertafter/insertbefore option of its
// own, so there is nothing to mirror there). The replacement block is
// always regenerated FROM SCRATCH out of this run's own args — an
// option set by an earlier run but not repeated in this one is DROPPED,
// not preserved from the existing block; this module does no per-field
// merge against whatever is already on disk, only a whole-block
// replace, so a playbook that manages one Host block across several
// tasks with different option subsets will see each task's own subset
// win outright rather than accumulate.
//
// Simplifications vs real ssh_config: no group/owner/mode/attributes
// support (this port never chowns/chmods a file it writes — see
// blockinfile.go's own simplifications list for the same narrowing); a
// `Host` header line is matched by an EXACT, single-alias, whitespace-
// trimmed comparison against "Host <host>" — a real ssh_config file's
// multi-pattern header ("Host foo bar") or differently-cased "host foo"
// is not recognized as the same block (real ssh_config's own Python
// implementation manages single-alias blocks exclusively too, so this
// matches its own practical usage, just not the full generality of the
// file format it edits); as with known_hosts.go's default path, a path
// beginning with "~" is handed to shellQuote as a literal string —
// since '~' is outside shellQuote's safe-character set the whole path
// gets single-quoted, which suppresses the target shell's own tilde
// expansion, so `user` in practice only works cleanly against targets
// where the resulting literal path happens to already be correct (a
// known, inherited limitation, not something newly introduced here);
// no add_keys_to_agent value validation beyond bool coercion (real
// add_keys_to_agent also accepts confirm/ask, which this port cannot
// represent since argBool only yields true/false); no diff_mode/
// hosts_added/hosts_changed/hosts_removed/hosts_change_diff return
// values (this port has no per-field diff machinery — see
// blockinfile.go and lineinfile.go for the same narrowing elsewhere in
// this package).
func moduleSSHConfig(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	host, err := requireString(args, "host")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("ssh_config: state must be present or absent, got %q", state)
	}
	path, err := sshConfigPath(args)
	if err != nil {
		return Result{}, err
	}

	current, err := fetchIfExists(ctx, conn, path)
	if err != nil {
		return Result{}, err
	}
	var lines []string
	if current != nil {
		lines = splitLines(string(current))
	}

	beginIdx, endIdx := sshConfigFindBlock(lines, host)
	existed := beginIdx >= 0

	var without []string
	if existed {
		without = append(append([]string{}, lines[:beginIdx]...), lines[endIdx+1:]...)
	} else {
		without = lines
	}

	if state == "absent" {
		if !existed {
			return Ok(host + " not present in " + path), nil
		}
		if err := writeRemote(ctx, conn, path, []byte(sshConfigJoin(without))); err != nil {
			return Result{}, err
		}
		return Changed(host + " removed from " + path), nil
	}

	blockLines, err := sshConfigBlockLines(args, host)
	if err != nil {
		return Result{}, err
	}

	insertIdx := len(without)
	if existed {
		insertIdx = beginIdx
	}
	result := append(append(append([]string{}, without[:insertIdx]...), blockLines...), without[insertIdx:]...)
	newContent := sshConfigJoin(result)

	if current != nil && newContent == string(current) {
		return Ok(host + " unchanged"), nil
	}
	if err := writeRemote(ctx, conn, path, []byte(newContent)); err != nil {
		return Result{}, err
	}
	if existed {
		return Changed(host + " updated in " + path), nil
	}
	return Changed(host + " added to " + path), nil
}

// sshConfigPath resolves the effective ssh_config path from args, per
// moduleSSHConfig's doc comment: ssh_config_file and user are mutually
// exclusive, and the default is "/etc/ssh/ssh_config".
func sshConfigPath(args map[string]any) (string, error) {
	configFile := argString(args, "ssh_config_file", "")
	user := argString(args, "user", "")
	if configFile != "" && user != "" {
		return "", errArg("ssh_config: ssh_config_file and user are mutually exclusive")
	}
	if configFile != "" {
		return configFile, nil
	}
	if user != "" {
		return "~" + user + "/.ssh/config", nil
	}
	return "/etc/ssh/ssh_config", nil
}

// sshConfigFindBlock locates an existing "Host <host>" block in lines
// (see moduleSSHConfig's doc comment on the exact-match, single-alias
// limitation), returning the indices of its header line and its last
// line before the next top-level Host line or EOF. Both are -1 if no
// such block is found.
func sshConfigFindBlock(lines []string, host string) (beginIdx, endIdx int) {
	header := "Host " + host
	beginIdx = -1
	for i, l := range lines {
		if strings.TrimSpace(l) == header {
			beginIdx = i
			break
		}
	}
	if beginIdx == -1 {
		return -1, -1
	}
	endIdx = len(lines) - 1
	for j := beginIdx + 1; j < len(lines); j++ {
		t := strings.TrimSpace(lines[j])
		if t == "Host" || strings.HasPrefix(t, "Host ") {
			endIdx = j - 1
			break
		}
	}
	return beginIdx, endIdx
}

// sshConfigBlockLines builds the full "Host <host>" block (header plus
// one indented line per configured option) from args, in a fixed field
// order so regenerating it is deterministic run over run — required for
// the plain string comparison moduleSSHConfig uses to detect "no
// change".
func sshConfigBlockLines(args map[string]any, host string) ([]string, error) {
	lines := []string{"Host " + host}
	add := func(option, value string) {
		if value != "" {
			lines = append(lines, "    "+option+" "+value)
		}
	}

	add("HostName", argString(args, "hostname", ""))
	add("Port", argString(args, "port", ""))
	add("User", argString(args, "remote_user", ""))
	add("IdentityFile", argString(args, "identity_file", ""))

	proxycommand := argString(args, "proxycommand", "")
	proxyjump := argString(args, "proxyjump", "")
	if proxycommand != "" && proxyjump != "" {
		return nil, errArg("ssh_config: proxycommand and proxyjump are mutually exclusive")
	}
	add("ProxyCommand", proxycommand)
	add("ProxyJump", proxyjump)

	for _, opt := range []struct{ key, option string }{
		{"forward_agent", "ForwardAgent"},
		{"add_keys_to_agent", "AddKeysToAgent"},
		{"identities_only", "IdentitiesOnly"},
	} {
		if _, ok := args[opt.key]; ok {
			lines = append(lines, "    "+opt.option+" "+sshConfigYesNo(argBool(args, opt.key, false)))
		}
	}

	add("AddressFamily", argString(args, "address_family", ""))
	add("ControlMaster", argString(args, "controlmaster", ""))
	add("ControlPath", argString(args, "controlpath", ""))
	add("ControlPersist", argString(args, "controlpersist", ""))
	add("DynamicForward", argString(args, "dynamicforward", ""))
	add("HostKeyAlgorithms", argString(args, "host_key_algorithms", ""))
	add("StrictHostKeyChecking", argString(args, "strict_host_key_checking", ""))
	add("UserKnownHostsFile", argString(args, "user_known_hosts_file", ""))

	otherOpts, err := sshConfigOtherOptions(args)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(otherOpts))
	for k := range otherOpts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		lines = append(lines, "    "+k+" "+otherOpts[k])
	}

	return lines, nil
}

// sshConfigOtherOptions validates and returns the "other_options" dict
// argument: keys must be lower case, values must be strings, matching
// real ssh_config's own documented constraints on this option.
func sshConfigOtherOptions(args map[string]any) (map[string]string, error) {
	v, ok := args["other_options"]
	if !ok || v == nil {
		return nil, nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, errArg("ssh_config: other_options must be a dict")
	}
	out := make(map[string]string, len(m))
	for k, val := range m {
		if k != strings.ToLower(k) {
			return nil, errArg("ssh_config: other_options key %q must be lower case", k)
		}
		s, ok := val.(string)
		if !ok {
			return nil, errArg("ssh_config: other_options[%q] must be a string", k)
		}
		out[k] = s
	}
	return out, nil
}

// sshConfigYesNo renders a bool as ssh_config's own "yes"/"no" literal.
func sshConfigYesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// sshConfigJoin renders lines back into file content, matching
// blockinfile.go's own trailing-newline convention.
func sshConfigJoin(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}
