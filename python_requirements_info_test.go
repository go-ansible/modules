package modules

import (
	"context"
	"testing"
)

// These tests run against the REAL local python3 (like command_test.go's
// use of remoteexec.Local) rather than a fakeConn: this module's whole
// point is introspecting whatever python3 actually has installed, so a
// scripted fake would only be testing the JSON-plumbing, not the
// composed script itself. `pip` is used as the probe package since pip
// ships itself as an installed distribution wherever python3's own pip
// bootstrap has run, which is assumed true in this repo's CI/dev
// environment (the same assumption command_test.go makes about `echo`
// and `pwd` being on PATH).

func TestModulePythonRequirementsInfoNoDependencies(t *testing.T) {
	conn := local()
	res, err := modulePythonRequirementsInfo(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("python_requirements_info must never report Changed")
	}
	if res.Extra["python"] == "" || res.Extra["python"] == nil {
		t.Fatalf("python = %v", res.Extra["python"])
	}
	vi, ok := res.Extra["python_version_info"].(map[string]any)
	if !ok || vi["major"] == nil {
		t.Fatalf("python_version_info = %v", res.Extra["python_version_info"])
	}
}

func TestModulePythonRequirementsInfoBareName(t *testing.T) {
	conn := local()
	res, err := modulePythonRequirementsInfo(context.Background(), conn, map[string]any{
		"dependencies": []any{"pip"},
	})
	if err != nil {
		t.Fatal(err)
	}
	valid := res.Extra["valid"].(map[string]any)
	entry, ok := valid["pip"].(map[string]any)
	if !ok {
		t.Fatalf("valid = %v, want a pip entry (pip is expected to be installed)", valid)
	}
	if entry["desired"] != nil {
		t.Fatalf("desired = %v, want nil for a bare module name", entry["desired"])
	}
}

func TestModulePythonRequirementsInfoSatisfiedSpec(t *testing.T) {
	conn := local()
	res, err := modulePythonRequirementsInfo(context.Background(), conn, map[string]any{
		"dependencies": []any{"pip>1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	valid := res.Extra["valid"].(map[string]any)
	if _, ok := valid["pip"]; !ok {
		t.Fatalf("valid = %v, want pip satisfied by pip>1", valid)
	}
}

func TestModulePythonRequirementsInfoMismatchedSpec(t *testing.T) {
	conn := local()
	res, err := modulePythonRequirementsInfo(context.Background(), conn, map[string]any{
		"dependencies": []any{"pip<1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	mismatched := res.Extra["mismatched"].(map[string]any)
	entry, ok := mismatched["pip"].(map[string]any)
	if !ok {
		t.Fatalf("mismatched = %v, want pip mismatched against pip<1", mismatched)
	}
	if entry["desired"] != "pip<1" {
		t.Fatalf("desired = %v", entry["desired"])
	}
}

func TestModulePythonRequirementsInfoNotFound(t *testing.T) {
	conn := local()
	res, err := modulePythonRequirementsInfo(context.Background(), conn, map[string]any{
		"dependencies": []any{"definitely-not-a-real-package-xyz"},
	})
	if err != nil {
		t.Fatal(err)
	}
	notFound := res.Extra["not_found"].([]any)
	if len(notFound) != 1 || notFound[0] != "definitely-not-a-real-package-xyz" {
		t.Fatalf("not_found = %v", notFound)
	}
}
