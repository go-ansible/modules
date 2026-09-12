package modules

// CheckModeKey is how the playbook engine tells a module it is running a
// dry run. Real Ansible passes the same flag to every module under this
// exact name, so a module reads it out of its own arguments rather than
// through a separate parameter — which is also why adding check mode did
// not change the signature every module in this package is written to.
const CheckModeKey = "_ansible_check_mode"

// InCheckMode reports whether args carry the dry-run flag.
func InCheckMode(args map[string]any) bool {
	v, _ := args[CheckModeKey].(bool)
	return v
}

// checkModeSupport lists the modules that actually honour a dry run.
// Everything absent from it is SKIPPED by the engine in check mode rather
// than run, which is exactly what real Ansible does with a module whose
// supports_check_mode is false — and it is what makes adding support one
// module at a time safe: the default for an unported module is "do
// nothing", never "modify anyway".
var checkModeSupport = map[string]bool{
	// Modules that decide what they would do and then skip doing it.
	"copy":     true,
	"template": true,

	// command and shell decline to run at all, reporting skipped.
	"command": true,
	"shell":   true,

	// Read-only modules support a dry run for free: they change nothing
	// whether or not check mode is on, so running them is both correct
	// and the useful thing to do — real Ansible reports debug as "ok" in
	// a check run, not "skipping". Anything that writes must earn its
	// place here with an implementation and a test instead.
	"debug": true,
	"fail":  true,
	"stat":  true,
	"find":  true,
	"slurp": true,
}

// Modules NOT listed above are skipped in check mode rather than run.
// file, lineinfile, blockinfile and replace are the obvious next ones:
// each mutates from several branches, and a dry run that is only mostly
// right is worse than one that honestly declines — so they are added one
// at a time, each with its own test, rather than declared supported and
// hoped about.

// SupportsCheckMode reports whether the named module honours a dry run.
// A fully-qualified collection name resolves like any other.
func SupportsCheckMode(name string) bool {
	return checkModeSupport[NormalizeName(name)]
}
