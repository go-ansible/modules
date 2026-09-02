package modules

import "testing"

// TestDocsCoversEveryRegisteredModule guards against docs_generated.go
// drifting out of sync with registry.go — if a module is added without
// running `go generate ./...`, this fails instead of ansible-doc
// silently having nothing to say about it.
func TestDocsCoversEveryRegisteredModule(t *testing.T) {
	for _, name := range Default().Names() {
		doc, ok := Docs[name]
		if !ok {
			t.Errorf("module %q has no entry in Docs — run `go generate ./...`", name)
			continue
		}
		if doc == "" {
			t.Errorf("module %q has an empty Docs entry", name)
		}
	}
}

func TestDocsHasNoExtraEntries(t *testing.T) {
	r := Default()
	for name := range Docs {
		if _, ok := r.Get(name); !ok {
			t.Errorf("Docs has an entry for %q, which isn't registered", name)
		}
	}
}
