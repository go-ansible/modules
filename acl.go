package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleAcl implements (a subset of) Ansible's `acl` module: sets,
// removes, or queries a POSIX ACL entry on a file or directory via the
// `setfacl`/`getfacl` commands.
//
// Args: path (string, required — real acl aliases this from `name`;
// this port only accepts `path`, since args here are already resolved
// by the caller before reaching a module — see known_hosts.go's doc
// comment for the same convention); entity (string, optional) — the
// user/group the ACL applies to; etype (user|group|mask|other,
// required for state=present/absent unless entry is given); permissions
// (string, optional) — e.g. "rwx", required for state=present unless
// entry is given; entry (string, optional) — real acl's deprecated raw
// "etype:qualifier:perms" form, used verbatim for state=present; state
// (present|absent|query, default "query"); recursive (bool, default
// false) — adds `-R`; default (bool, default false) — act on the
// directory's default ACL instead of its access ACL; follow (bool,
// default true) — when false, adds `-h` (act on a symlink itself
// rather than its target); recalculate_mask (default|mask|no_mask,
// default "default") — only "no_mask" has an effect here (adds `-n` to
// suppress setfacl's automatic mask recalculation); "mask" is treated
// the same as "default" (GNU setfacl always recalculates the mask
// unless told not to, so there's no separate flag for "force
// recalculate").
//
// Simplifications vs real acl: no use_nfsv4_acls (this port always
// uses POSIX ACLs via setfacl/getfacl, never nfs4_setfacl/nfs4_getfacl
// — Linux-only, matching real acl's own "Linux distributions only"
// note). For state=absent/present via etype/entity/permissions,
// idempotency is checked by comparing against getfacl's own output
// format exactly; when the deprecated raw `entry` form is used instead,
// no idempotency check is performed at all (this port always runs
// setfacl and reports changed) — parsing entry's shorthand forms
// ("u:bob:rwx", "default:user:joe:rw-", "-" placeholder permissions)
// well enough to compare against getfacl's canonical output was judged
// out of scope; a real gap versus real acl's behavior, documented
// rather than silently claimed.
func moduleAcl(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	path, err := requireString(args, "path")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "query")
	recursive := argBool(args, "recursive", false)
	isDefault := argBool(args, "default", false)
	follow := argBool(args, "follow", true)
	recalc := argString(args, "recalculate_mask", "default")

	var flags strings.Builder
	if recursive {
		flags.WriteString(" -R")
	}
	if !follow {
		flags.WriteString(" -h")
	}
	if recalc == "no_mask" {
		flags.WriteString(" -n")
	}

	switch state {
	case "query":
		acl, err := getfaclEntries(ctx, conn, path)
		if err != nil {
			return Result{}, err
		}
		return Ok("").WithExtra("acl", acl), nil

	case "present":
		entry := argString(args, "entry", "")
		var spec, wantLine string
		if entry != "" {
			spec = entry
		} else {
			etype, err := requireString(args, "etype")
			if err != nil {
				return Result{}, errArg("acl: etype is required when entry is not given")
			}
			permissions, err := requireString(args, "permissions")
			if err != nil {
				return Result{}, errArg("acl: permissions is required when entry is not given")
			}
			entity := argString(args, "entity", "")
			wantLine = aclLine(isDefault, etype, entity, permissions)
			spec = aclSpec(isDefault, etype, entity, permissions)

			acl, err := getfaclEntries(ctx, conn, path)
			if err != nil {
				return Result{}, err
			}
			for _, l := range acl {
				if l == wantLine {
					return Ok("acl entry already present"), nil
				}
			}
		}
		cmd := "setfacl" + flags.String() + " -m " + shellQuote(spec) + " " + shellQuote(path)
		if _, err := run(ctx, conn, cmd); err != nil {
			return Result{}, err
		}
		acl, err := getfaclEntries(ctx, conn, path)
		if err != nil {
			return Result{}, err
		}
		return Changed("acl entry set").WithExtra("acl", acl), nil

	case "absent":
		entry := argString(args, "entry", "")
		var spec, wantPrefix string
		if entry != "" {
			// Strip a trailing ":perms" field, if any, since -x takes no
			// permissions — a best-effort adaptation of the deprecated
			// raw form (see the doc comment above).
			spec = entry
			if i := strings.LastIndex(spec, ":"); i >= 0 {
				spec = spec[:i]
			}
		} else {
			etype, err := requireString(args, "etype")
			if err != nil {
				return Result{}, errArg("acl: etype is required when entry is not given")
			}
			entity := argString(args, "entity", "")
			wantPrefix = aclSpec(isDefault, etype, entity, "")
			spec = aclRemoveSpec(isDefault, etype, entity)

			acl, err := getfaclEntries(ctx, conn, path)
			if err != nil {
				return Result{}, err
			}
			found := false
			for _, l := range acl {
				if strings.HasPrefix(l, wantPrefix) {
					found = true
					break
				}
			}
			if !found {
				return Ok("acl entry already absent"), nil
			}
		}
		cmd := "setfacl" + flags.String() + " -x " + shellQuote(spec) + " " + shellQuote(path)
		if _, err := run(ctx, conn, cmd); err != nil {
			return Result{}, err
		}
		acl, err := getfaclEntries(ctx, conn, path)
		if err != nil {
			return Result{}, err
		}
		return Changed("acl entry removed").WithExtra("acl", acl), nil

	default:
		return Result{}, errArg("acl: state must be present, absent, or query, got %q", state)
	}
}

// aclLine builds the exact line getfacl would print for this entry —
// used to check whether it's already present.
func aclLine(isDefault bool, etype, entity, permissions string) string {
	prefix := ""
	if isDefault {
		prefix = "default:"
	}
	return prefix + etype + ":" + entity + ":" + permissions
}

// aclSpec builds the setfacl -m entry spec ("[d:]etype:entity:perms").
func aclSpec(isDefault bool, etype, entity, permissions string) string {
	prefix := ""
	if isDefault {
		prefix = "d:"
	}
	return prefix + etype + ":" + entity + ":" + permissions
}

// aclRemoveSpec builds the setfacl -x entry spec ("[d:]etype:entity",
// no permissions field).
func aclRemoveSpec(isDefault bool, etype, entity string) string {
	prefix := ""
	if isDefault {
		prefix = "d:"
	}
	return prefix + etype + ":" + entity
}

// getfaclEntries runs `getfacl` on path and returns its ACL entry lines
// (comment lines starting with "#" and blank lines are dropped).
func getfaclEntries(ctx context.Context, conn remoteexec.Connection, path string) ([]string, error) {
	out, err := run(ctx, conn, "getfacl "+shellQuote(path))
	if err != nil {
		return nil, err
	}
	var entries []string
	for _, line := range splitLines(out) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		entries = append(entries, line)
	}
	return entries, nil
}
