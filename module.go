// Package modules implements Ansible's module execution model: each
// module is a Go function that takes a target connection
// (github.com/go-remoteexec/transport) and a set of arguments (already
// Jinja2-rendered by the caller) and returns a Result — Ansible's
// changed/failed/msg triple plus any module-specific fields.
//
// Unlike real Ansible, which copies a Python script to the target and
// runs it there, a module here runs its logic on the control node and
// reaches the target only through the Connection's Exec/Put/Fetch
// primitives. The observable behavior is the same (the target ends up
// in the same state); the difference is architectural, not behavioral,
// and it means a module needs no Go toolchain on the target.
package modules

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	remoteexec "github.com/go-remoteexec/transport"
)

// Result is a module's outcome: Ansible's changed/failed/msg triple,
// plus optional facts (merged into ansible_facts, e.g. by set_fact) and
// module-specific extra fields (e.g. command's stdout/stderr/rc).
type Result struct {
	Changed bool
	Failed  bool

	// Skipped marks work a module declined to do rather than failed at —
	// today, a command it will not run because this is a dry run. The
	// engine reports it the way it reports any other skipped task.
	Skipped bool

	Msg string

	// NoMsg says this module's result carries NO "msg" key at all,
	// which is not the same as an empty one. Real's `file` returns
	// state/mode/owner and no message, so `r.msg` there is an
	// UNDEFINED variable; real's `command` returns an empty message,
	// so `r.msg` is "". A port that always writes the key is lenient
	// where real raises, and one that omits it whenever it is empty
	// breaks command -- measured, both ways round. So the module that
	// omits it says so.
	NoMsg bool

	Facts map[string]any
	Extra map[string]any

	// Diffs is what changed, for a run started with --diff. A module
	// fills it only when InDiffMode says the caller asked; it is a
	// slice because real Ansible's own callback accepts a list, for a
	// module that touches more than one file.
	Diffs []Diff
}

// WithDiff returns a copy of r with d appended to Diffs, or r unchanged
// when d carries nothing worth showing. Modules call it unconditionally
// and let it decide, which keeps the diff-mode test in one place.
func (r Result) WithDiff(d Diff) Result {
	if d.Empty() {
		return r
	}
	out := r
	out.Diffs = append(append([]Diff(nil), r.Diffs...), d)
	return out
}

// Ok returns a successful, unchanged result.
func Ok(msg string) Result { return Result{Msg: msg} }

// Changed returns a successful, changed result.
func Changed(msg string) Result { return Result{Changed: true, Msg: msg} }

// Skipped returns a result for work the module declined to do.
func Skipped(msg string) Result { return Result{Skipped: true, Msg: msg} }

// Fail returns a failed result. Modules normally return this alongside
// a non-nil error only when the failure is unexpected (a connection
// error, an unreadable file); an expected, well-formed failure (e.g.
// the `fail` module itself, or `assert` on a false condition) returns
// it with a nil error, since it is not the module's own execution that
// went wrong.
func Fail(msg string) Result { return Result{Failed: true, Msg: msg} }

// WithExtra returns a copy of r with key set in Extra.
func (r Result) WithExtra(key string, value any) Result {
	out := r
	out.Extra = make(map[string]any, len(r.Extra)+1)
	for k, v := range r.Extra {
		out.Extra[k] = v
	}
	out.Extra[key] = value
	return out
}

// Func is a module's entry point. ctx carries cancellation/timeout;
// conn is already connected to the task's target; args is the task's
// parameters, already Jinja2-rendered by the caller (this package never
// templates anything itself). A non-nil error means the module could
// not determine an outcome at all (a transport failure); an expected
// failure is a Result with Failed=true and a nil error.
type Func func(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error)

// Registry maps module names to their Func.
type Registry struct {
	mu      sync.RWMutex
	modules map[string]Func
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{modules: map[string]Func{}}
}

// Register adds fn under name, replacing any existing module of the
// same name (so a caller can override a built-in with a custom module).
func (r *Registry) Register(name string, fn Func) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.modules[name] = fn
}

// Get looks up a module by name. A fully-qualified collection name
// (FQCN) — "ansible.builtin.copy", "community.general.ufw" — resolves
// to the same entry as the bare name ("copy", "ufw") once the known
// collection prefix is stripped; see NormalizeName. This is a
// deliberate simplification, not full collection-scoped resolution:
// this registry is a single flat namespace (matching how this port's
// module set has no real cross-collection name collisions to
// disambiguate), so an FQCN with the WRONG collection prefix for a
// given module (e.g. "ansible.builtin.ufw", when ufw is actually
// community.general's) still resolves — real Ansible would instead
// fail "couldn't resolve module" in that case. In practice this only
// diverges from real Ansible on a playbook that already has an
// incorrect FQCN, which would already be broken there too.
func (r *Registry) Get(name string) (Func, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if fn, ok := r.modules[name]; ok {
		return fn, ok
	}
	if short := NormalizeName(name); short != name {
		fn, ok := r.modules[short]
		return fn, ok
	}
	return nil, false
}

// knownCollectionPrefixes are the collections this port's module
// registry draws from, plus "ansible.legacy." — real Ansible's own
// alias for ansible.builtin, commonly seen in generated/exported
// playbooks. Deliberately NOT a wildcard match on "any two dotted
// segments before the last one": limiting to known collections avoids
// mistaking an unrelated dotted string for an FQCN.
var knownCollectionPrefixes = []string{
	"ansible.legacy.",
	"ansible.builtin.",
	"ansible.posix.",
	"community.general.",
}

// NormalizeName strips a known collection prefix from an FQCN module
// or playbook-directive reference, returning name unchanged if it
// carries none of them.
func NormalizeName(name string) string {
	for _, prefix := range knownCollectionPrefixes {
		if short, ok := strings.CutPrefix(name, prefix); ok {
			return short
		}
	}
	return name
}

// Names returns every registered module name, sorted.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.modules))
	for n := range r.modules {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Run looks up name and runs it, returning a Result{Failed:true} (not a
// Go error) for an unknown module name — matching Ansible's own
// "couldn't resolve module" being a task failure, not a crash.
func (r *Registry) Run(ctx context.Context, name string, conn remoteexec.Connection, args map[string]any) (Result, error) {
	fn, ok := r.Get(name)
	if !ok {
		return Fail(fmt.Sprintf("the module %s was not found", name)), nil
	}
	res, err := fn(ctx, conn, args)
	if err != nil {
		return res, err
	}
	return finalizeOutput(res), nil
}

// finalizeOutput reproduces what real Ansible's AnsibleModule.exit_json
// does to every module result on its way out, rather than leaving each
// module to remember it: a trailing newline is trimmed off stdout and
// stderr, and a matching stdout_lines/stderr_lines is added.
//
// Both halves were measured against real ansible-core 2.21.4. `echo
// hello` gives a 5-character stdout there and gave 6 here, and
// `result.stdout_lines` — which real playbooks use constantly — did not
// exist at all.
//
// The trim is exactly Python's rstrip("\r\n"): every trailing carriage
// return and newline goes, and nothing else does. Trailing spaces and
// tabs survive (`printf 'x  \t '` stays 5 characters) and leading
// newlines survive (`printf '\n\nx'` stays 3), both confirmed against
// real Ansible rather than assumed.
func finalizeOutput(res Result) Result {
	for _, key := range []string{"stdout", "stderr"} {
		raw, ok := res.Extra[key]
		if !ok {
			continue
		}
		s, ok := raw.(string)
		if !ok {
			continue
		}
		s = strings.TrimRight(s, "\r\n")
		res = res.WithExtra(key, s)
		res = res.WithExtra(key+"_lines", outputLines(s))
	}
	return res
}

// outputLines is Python's str.splitlines(): no trailing empty element, and
// an empty string yields an empty list rather than a one-element one.
func outputLines(s string) []any {
	if s == "" {
		return []any{}
	}
	parts := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	out := make([]any, 0, len(parts))
	for _, p := range parts {
		out = append(out, p)
	}
	return out
}

// Default returns a Registry pre-populated with this package's built-in
// module set.
func Default() *Registry {
	r := NewRegistry()
	registerBuiltins(r)
	return r
}
