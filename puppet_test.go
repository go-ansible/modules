package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

const puppetAgentCmd = "timeout -s 9 30m puppet agent --onetime --no-daemonize --no-usecacheonfailure " +
	"--no-splay --detailed-exitcodes --verbose --color 0"

func TestModulePuppetNoChanges(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"puppet config print agent_disabled_lockfile":                    {RC: 0, Stdout: "/opt/puppetlabs/puppet/cache/state/agent_disabled.lock\n"},
		"test -e /opt/puppetlabs/puppet/cache/state/agent_disabled.lock": {RC: 1},
		"LANG=C " + puppetAgentCmd:                                       {RC: 0, Stdout: "no changes\n"},
	})
	res, err := modulePuppet(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["rc"] != 0 {
		t.Fatalf("rc = %v", res.Extra["rc"])
	}
}

func TestModulePuppetChanges(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"puppet config print agent_disabled_lockfile": {RC: 0, Stdout: "/lock\n"},
		"test -e /lock":            {RC: 1},
		"LANG=C " + puppetAgentCmd: {RC: 2, Stdout: "changes made\n"},
	})
	res, err := modulePuppet(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["rc"] != 0 {
		t.Fatalf("rc should be normalized to 0, got %v", res.Extra["rc"])
	}
}

func TestModulePuppetDisabledIsNotFailed(t *testing.T) {
	// rc==1 uses exit_json in real puppet.py, not fail_json: surprising
	// but faithfully replicated (see modulePuppet's own doc comment).
	conn := newFakeConn(map[string]remoteexec.Result{
		"puppet config print agent_disabled_lockfile": {RC: 0, Stdout: "/lock\n"},
		"test -e /lock":            {RC: 1},
		"LANG=C " + puppetAgentCmd: {RC: 1, Stdout: "administratively disabled\n"},
	})
	res, err := modulePuppet(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v, want NOT failed (matches real puppet.py's exit_json on rc==1)", res)
	}
	if res.Extra["disabled"] != true || res.Extra["error"] != true {
		t.Fatalf("res.Extra = %+v", res.Extra)
	}
}

func TestModulePuppetAdministrativelyDisabledPreCheck(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"puppet config print agent_disabled_lockfile": {RC: 0, Stdout: "/lock\n"},
		"test -e /lock": {RC: 0},
	})
	res, err := modulePuppet(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want failed when the lockfile exists")
	}
}

func TestModulePuppetOtherFailureRC(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"puppet config print agent_disabled_lockfile": {RC: 0, Stdout: "/lock\n"},
		"test -e /lock":            {RC: 1},
		"LANG=C " + puppetAgentCmd: {RC: 3, Stderr: "boom\n"},
	})
	res, err := modulePuppet(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want failed for rc==3")
	}
}

func TestModulePuppetApplyManifest(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e /var/lib/example.pp": {RC: 0},
		"LANG=C timeout -s 9 30m puppet apply --detailed-exitcodes /var/lib/example.pp": {RC: 0},
	})
	res, err := modulePuppet(context.Background(), conn, map[string]any{"manifest": "/var/lib/example.pp"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModulePuppetManifestMissing(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e /var/lib/example.pp": {RC: 1},
	})
	res, err := modulePuppet(context.Background(), conn, map[string]any{"manifest": "/var/lib/example.pp"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want failed for missing manifest")
	}
}

func TestModulePuppetPuppetmasterMutuallyExclusive(t *testing.T) {
	conn := newFakeConn(nil)
	_, err := modulePuppet(context.Background(), conn, map[string]any{
		"puppetmaster": "master.example.com", "manifest": "/x.pp",
	})
	if err == nil {
		t.Fatal("want error")
	}
}

func TestModulePuppetExecute(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"LANG=C timeout -s 9 30m puppet apply --detailed-exitcodes --execute 'include ::mymodule'": {RC: 0},
	})
	res, err := modulePuppet(context.Background(), conn, map[string]any{"execute": "include ::mymodule"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
}
