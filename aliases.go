package modules

import "fmt"

// argAlias is one entry of a real module's argument_spec that declares
// `aliases=[...]`: the option's canonical name and the other spellings a
// playbook may use for it, in the order the real argument_spec lists
// them. The order is not cosmetic -- see resolveAliases.
type argAlias struct {
	canonical string
	aliases   []string
}

// moduleArgAliases is the alias table resolveAliases applies, keyed by
// the module's short registered name (an FQCN is normalised first, the
// same way Registry.Get resolves it).
//
// Every entry is copied from the real module's own argument_spec, not
// from memory and not from the rendered docs page alone:
//
//   - file, stat:                       ansible-core stable-2.21,
//     lib/ansible/modules/{file,stat}.py
//   - lineinfile, blockinfile, replace: ansible-core stable-2.21,
//     lib/ansible/modules/{lineinfile,blockinfile,replace}.py
//   - known_hosts, subversion:          ansible-core stable-2.21,
//     lib/ansible/modules/{known_hosts,subversion}.py
//   - acl:                              ansible.posix, plugins/modules/acl.py
//   - bzr, logrotate:                   community.general,
//     plugins/modules/{bzr,logrotate}.py
//
// The non-path aliases of those same modules (lineinfile's regex/value,
// blockinfile's content, stat's mime/attr/checksum families, subversion's
// rev/version, acl's recurse) are listed too, so that a playbook's
// spelling of an option gets exactly the treatment its canonical name
// gets here -- read where this port reads it, and ignored where this
// port ignores it (stat's get_attributes and checksum_algorithm, for
// instance, are not implemented; their aliases are resolved onto names
// nothing reads, which is the same outcome as passing the canonical
// name today).
//
// Two modules that DO have real aliases are deliberately absent, because
// they already resolve them inline and this table would change their
// behaviour rather than fix it:
//
//   - hponcfg reads `path` and falls back to `src`. Real hponcfg.py's
//     argument_spec is src=dict(aliases=["path"]) (its DOCUMENTATION
//     block says the reverse), so with both given real uses `path`'s
//     value -- which is what the inline fallback already does (minus
//     real's "Both option" warning).
//   - vmadm reads `name` and falls back to `alias`, and passes every
//     remaining arg through to vmadm's JSON payload by its own key, so
//     copying `alias` onto `name` here would add a payload field.
//
// Modules not in this table behave exactly as before: their args reach
// them untouched. Many modules in this package resolve their own aliases
// inline (`argString(args, "x", argString(args, "y", ""))`); that keeps
// working, and moving them here is a separate, mechanical change.
var moduleArgAliases = map[string][]argAlias{
	"file": {
		{"path", []string{"dest", "name"}},
	},
	"stat": {
		{"path", []string{"dest", "name"}},
		{"get_mime", []string{"mime", "mime_type", "mime-type"}},
		{"get_attributes", []string{"attr", "attributes"}},
		{"checksum_algorithm", []string{"checksum", "checksum_algo"}},
	},
	"lineinfile": {
		{"path", []string{"dest", "destfile", "name"}},
		{"regexp", []string{"regex"}},
		{"line", []string{"value"}},
	},
	"blockinfile": {
		{"path", []string{"dest", "destfile", "name"}},
		{"block", []string{"content"}},
	},
	"replace": {
		{"path", []string{"dest", "destfile", "name"}},
	},
	"known_hosts": {
		{"name", []string{"host"}},
	},
	"subversion": {
		{"repo", []string{"name", "repository"}},
		{"revision", []string{"rev", "version"}},
	},
	"acl": {
		{"path", []string{"name"}},
		{"recursive", []string{"recurse"}},
	},
	"bzr": {
		{"name", []string{"parent"}},
	},
	"logrotate": {
		{"name", []string{"config_name"}},
	},
}

// resolveAliases returns args with every alias in moduleArgAliases for
// module copied onto its canonical name, plus the warnings real Ansible
// would emit while doing so. It is what real's AnsibleModule does to
// module.params before the module body runs, done once here in
// Registry.Run so that no module has to remember it.
//
// It mirrors ansible-core's _handle_aliases
// (lib/ansible/module_utils/common/parameters.py, stable-2.21) line for
// line, including the parts that are easy to "improve" by accident:
//
//   - Both the canonical name and an alias given is NOT an error. Real
//     appends (option, alias) to alias_warnings and then assigns
//     parameters[k] = parameters[alias] anyway, so the ALIAS's value wins
//     -- whether or not the two values differ. ModuleArgumentSpecValidator
//     (module_utils/common/arg_spec.py) then turns each pair into the
//     warning "Both option {option} and its alias {alias} are set.",
//     reproduced here word for word.
//   - Aliases are visited in argument_spec order, and parameters is
//     mutated as it goes. So with two aliases of one option and no
//     canonical name (file with both dest and name), the first alias
//     sets path, the second then finds path already set, warns, and
//     overwrites it: the LAST alias in the spec's list wins and one
//     warning names it.
//   - The alias key itself is not removed. Real leaves it in
//     module.params too; here that keeps every key a module could read
//     before this change readable after it, and an alias a module never
//     reads stays as inert as it always was.
//
// The caller's map is never modified: when anything needs copying, a
// shallow copy is made first, since the playbook engine may reuse a
// task's args (a loop, a retry).
func resolveAliases(module string, args map[string]any) (map[string]any, []string) {
	spec, ok := moduleArgAliases[NormalizeName(module)]
	if !ok || len(args) == 0 {
		return args, nil
	}
	var out map[string]any
	var warnings []string
	for _, opt := range spec {
		for _, alias := range opt.aliases {
			src := args
			if out != nil {
				src = out
			}
			v, given := src[alias]
			if !given {
				continue
			}
			if out == nil {
				out = make(map[string]any, len(args)+1)
				for k, val := range args {
					out[k] = val
				}
			}
			if _, both := out[opt.canonical]; both {
				warnings = append(warnings,
					fmt.Sprintf("Both option %s and its alias %s are set.", opt.canonical, alias))
			}
			out[opt.canonical] = v
		}
	}
	if out == nil {
		return args, nil
	}
	return out, warnings
}

// withWarnings puts warnings at the front of res's "warnings" extra,
// ahead of any the module itself reported -- real emits alias warnings
// while validating arguments, before the module body has run. find and
// cobbler_system already report warnings under that key, one as []any
// and one as []string; both are kept, as []any, which is the shape the
// key has after a JSON round trip anyway.
func withWarnings(res Result, warnings []string) Result {
	if len(warnings) == 0 {
		return res
	}
	merged := make([]any, 0, len(warnings))
	for _, w := range warnings {
		merged = append(merged, w)
	}
	switch existing := res.Extra["warnings"].(type) {
	case nil:
	case []any:
		merged = append(merged, existing...)
	case []string:
		for _, w := range existing {
			merged = append(merged, w)
		}
	default:
		merged = append(merged, existing)
	}
	return res.WithExtra("warnings", merged)
}
