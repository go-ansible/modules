package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleIpaSubcaCreate(t *testing.T) {
	showCmd := "ipa ca-show AnsibleSubCA1 --all --raw"
	addCmd := "ipa ca-add AnsibleSubCA1 --ipacasubjectdn=CN=AnsibleSubCA1,O=example.com '--description=Ansible Sub CA'"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 2},
		addCmd:           {RC: 0},
	})
	res, err := moduleIpaSubca(context.Background(), fc, map[string]any{
		"subca_name": "AnsibleSubCA1", "subca_subject": "CN=AnsibleSubCA1,O=example.com",
		"subca_desc": "Ansible Sub CA",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleIpaSubcaCreateMissingSubject(t *testing.T) {
	showCmd := "ipa ca-show AnsibleSubCA1 --all --raw"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 2},
	})
	if _, err := moduleIpaSubca(context.Background(), fc, map[string]any{
		"subca_name": "AnsibleSubCA1",
	}); err == nil {
		t.Fatal("want error: subca_subject required to create")
	}
}

func TestModuleIpaSubcaAbsent(t *testing.T) {
	showCmd := "ipa ca-show AnsibleSubCA1 --all --raw"
	delCmd := "ipa ca-del AnsibleSubCA1"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 0, Stdout: "  cn: AnsibleSubCA1\n"},
		delCmd:           {RC: 0},
	})
	res, err := moduleIpaSubca(context.Background(), fc, map[string]any{
		"subca_name": "AnsibleSubCA1", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleIpaSubcaDisable(t *testing.T) {
	showCmd := "ipa ca-show AnsibleSubCA1 --all --raw"
	disableCmd := "ipa ca-disable AnsibleSubCA1"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 0, Stdout: "  cn: AnsibleSubCA1\n"},
		disableCmd:       {RC: 0},
	})
	res, err := moduleIpaSubca(context.Background(), fc, map[string]any{
		"subca_name": "AnsibleSubCA1", "state": "disabled",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleIpaSubcaSubjectNotResent(t *testing.T) {
	showCmd := "ipa ca-show AnsibleSubCA1 --all --raw"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 0, Stdout: "  cn: AnsibleSubCA1\n  ipacasubjectdn: CN=Old,O=example.com\n"},
	})
	res, err := moduleIpaSubca(context.Background(), fc, map[string]any{
		"subca_name": "AnsibleSubCA1", "subca_subject": "CN=New,O=example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	// subca_subject is never diffed/sent on an existing sub-CA (real ipa_subca
	// explicitly drops it), so no ca-mod call should have happened and the
	// result must be unchanged.
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	for _, c := range fc.Commands {
		if c != showCmd && c != "command -v ipa" {
			t.Fatalf("unexpected command %q, subca_subject should never be sent to ca-mod", c)
		}
	}
}
