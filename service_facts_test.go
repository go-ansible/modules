package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleServiceFacts(t *testing.T) {
	listOut := "sshd.service    loaded active running OpenSSH server\n" +
		"cron.service     loaded active running Command scheduler\n" +
		"bad.service      loaded failed failed  A failed unit\n" +
		"● red.service     loaded failed failed  Marked failed unit\n"
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v systemctl >/dev/null 2>&1":                          {RC: 0},
		"systemctl list-units --type=service --all --no-legend --plain": {RC: 0, Stdout: listOut},
	})
	res, err := moduleServiceFacts(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	services, ok := res.Extra["services"].(map[string]any)
	if !ok {
		t.Fatalf("Extra[services] = %#v", res.Extra["services"])
	}
	sshd, ok := services["sshd.service"].(map[string]any)
	if !ok || sshd["state"] != "started" || sshd["source"] != "systemd" {
		t.Fatalf("sshd = %#v", services["sshd.service"])
	}
	bad, ok := services["bad.service"].(map[string]any)
	if !ok || bad["state"] != "stopped" {
		t.Fatalf("bad = %#v", services["bad.service"])
	}
	red, ok := services["red.service"].(map[string]any)
	if !ok || red["state"] != "stopped" {
		t.Fatalf("red (with leading marker) = %#v", services["red.service"])
	}
}

// This test used to assert res.Failed, which pinned THIS PORT's
// behaviour rather than real's. Measured against ansible-core 2.21.4
// on a host with no systemd, real does not fail -- it SKIPS:
//
//	results = dict(skipped=True, msg="Failed to find any services. "
//	               "This can be due to privileges or some other configuration issue.")
//
// and its key set is changed,failed,msg,skipped. The difference is not
// cosmetic: a failed task stops a play that a skipped one does not, so
// the old behaviour would halt a playbook real lets through.
func TestModuleServiceFactsNoSystemdSkipsRatherThanFails(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v systemctl >/dev/null 2>&1": {RC: 1},
	})
	res, err := moduleServiceFacts(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Error("reported failed; real skips, and a failure would halt a play real lets through")
	}
	if !res.Skipped {
		t.Error("did not report skipped")
	}
	if res.Msg != "Failed to find any services. This can be due to privileges or some other configuration issue." {
		t.Errorf("msg = %q, want real's own wording", res.Msg)
	}
}

func TestModuleServiceFactsSkipsShortLines(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v systemctl >/dev/null 2>&1":                          {RC: 0},
		"systemctl list-units --type=service --all --no-legend --plain": {RC: 0, Stdout: "\ntooshort\n"},
	})
	res, err := moduleServiceFacts(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	services := res.Extra["services"].(map[string]any)
	if len(services) != 0 {
		t.Fatalf("services = %#v, want empty", services)
	}
}
