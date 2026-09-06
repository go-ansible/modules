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
	Msg     string
	Facts   map[string]any
	Extra   map[string]any
}

// Ok returns a successful, unchanged result.
func Ok(msg string) Result { return Result{Msg: msg} }

// Changed returns a successful, changed result.
func Changed(msg string) Result { return Result{Changed: true, Msg: msg} }

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
	return fn(ctx, conn, args)
}

// Default returns a Registry pre-populated with this package's built-in
// module set.
func Default() *Registry {
	r := NewRegistry()
	registerBuiltins(r)
	return r
}
