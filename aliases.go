package modules

// Parameter aliases, resolved in ONE place before dispatch.
//
// Real Ansible declares aliases in each module's own argument_spec and
// AnsibleModule resolves them before the module body ever runs
// (`module_utils/common/parameters.py:_handle_aliases`). Nothing in this
// package did: every module read the canonical key and only the canonical
// key, so a task written against the documented alias failed with
// "missing required argument: path" before doing anything
// (go-ansible/modules#110). Several doc comments here even asserted that
// arguments "are already resolved by the caller before reaching a module" —
// no caller resolved them, which made the sentence the only thing standing
// between the claim and the defect.
//
// # Real's resolution rule, which is not the obvious one
//
// From parameters.py:240-245:
//
//	for alias in aliases:
//	    aliases_results[alias] = k
//	    if alias in parameters:
//	        if k in parameters and alias_warnings is not None:
//	            alias_warnings.append((k, alias))
//	        parameters[k] = parameters[alias]
//
// Three consequences, all reproduced below:
//
//   - The assignment is unconditional, so when BOTH the canonical name and an
//     alias are given, THE ALIAS WINS. A reader expecting the explicit
//     canonical value to take precedence has it backwards.
//   - Aliases are applied in declaration order, so with two aliases present
//     the LAST one in that order wins. The order in this table is therefore
//     real's own order, not alphabetical.
//   - The alias key stays in the parameters alongside the canonical one. A
//     module that happens to read the alias directly keeps working.
//
// Real also emits a warning when a canonical and its alias are both given.
// This package has no warning channel, so that half is not reproduced; the
// precedence is.
//
// # Why this table is short
//
// It holds what has been READ in ansible-core 2.21.4, module by module, and
// nothing else. Real declares aliases on hundreds of parameters across the
// collections this port covers, and a table written from guesswork would
// turn a loud "missing required argument" into a silently ignored value,
// which is worse than the defect it replaces. Entries are added when the
// module's own argument_spec has been opened — the line is cited beside each
// group.
var moduleArgAliases = map[string]map[string][]string{
	// file.py:971
	"file": {"path": {"dest", "name"}},

	// stat.py:454,457,458,462
	"stat": {
		"path":               {"dest", "name"},
		"get_mime":           {"mime", "mime_type", "mime-type"},
		"get_attributes":     {"attr", "attributes"},
		"checksum_algorithm": {"checksum", "checksum_algo"},
	},

	// lineinfile.py:572,574,576
	"lineinfile": {
		"path":   {"dest", "destfile", "name"},
		"regexp": {"regex"},
		"line":   {"value"},
	},

	// blockinfile.py:244,247
	"blockinfile": {
		"path":  {"dest", "destfile", "name"},
		"block": {"content"},
	},

	// replace.py:226
	"replace": {"path": {"dest", "destfile", "name"}},

	// systemd_service.py:346. Registered under two names in registry.go, and
	// a table keyed on the name the task used has to carry both. The third
	// registration, `service`, is deliberately absent: real's `service` is a
	// DIFFERENT module with its own argument_spec, which has not been opened
	// here, and inventing its alias set is exactly what the note above
	// forbids. moduleSystemd also resolves these itself, for the direct
	// callers its own tests use; these entries are what make the DISPATCHED
	// path behave the same.
	"systemd":         {"name": {"service", "unit"}},
	"systemd_service": {"name": {"service", "unit"}},

	// The four modules whose own doc comments asserted this convention
	// before anything implemented it, each now verified against the spec
	// rather than against the comment that described it:
	//
	//	known_hosts.py:363   name=dict(..., aliases=['host'])
	//	bzr.py:144           name=dict(..., aliases=["parent"])
	//	logrotate.py:893     name=dict(..., aliases=["config_name"])
	//	subversion.py:296-297
	//	                     repo=dict(..., aliases=['name', 'repository'])
	//	                     revision=dict(..., aliases=['rev', 'version'])
	//
	// Reading the specs earned one correction: subversion aliases its
	// REVISION too, which none of this package's comments mentioned.
	"known_hosts": {"name": {"host"}},
	"bzr":         {"name": {"parent"}},
	"logrotate":   {"name": {"config_name"}},
	"subversion": {
		"repo":     {"name", "repository"},
		"revision": {"rev", "version"},
	},
}

// resolveAliases returns args with each known alias copied onto its canonical
// name, following real's precedence. The input map is never modified: the
// engine may reuse it (for a loop, or for a retry), and a module that saw a
// mutated copy of someone else's map would be a different bug.
//
// The returned map is args itself when there is nothing to do, which is the
// common case — the copy is paid for only by a task that actually used an
// alias.
func resolveAliases(module string, args map[string]any) map[string]any {
	table, ok := moduleArgAliases[NormalizeName(module)]
	if !ok || len(args) == 0 {
		return args
	}
	var out map[string]any
	for canonical, aliases := range table {
		for _, alias := range aliases {
			v, present := args[alias]
			if !present {
				continue
			}
			if out == nil {
				out = make(map[string]any, len(args)+len(table))
				for k, vv := range args {
					out[k] = vv
				}
			}
			// Unconditional, as real does it: the alias wins over an
			// explicitly given canonical, and a later alias wins over an
			// earlier one.
			out[canonical] = v
		}
	}
	if out == nil {
		return args
	}
	return out
}
