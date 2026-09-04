package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func aptRpmToolsPresent() map[string][]remoteexec.Result {
	return map[string][]remoteexec.Result{
		"test -e /usr/bin/apt-get": {{RC: 0}},
		"test -e /usr/bin/rpm":     {{RC: 0}},
	}
}

func TestModuleAptRpmInstallBasic(t *testing.T) {
	on := aptRpmToolsPresent()
	on["rpm -q --provides foo >/dev/null 2>&1"] = []remoteexec.Result{{RC: 1}, {RC: 0}}
	on["env LANGUAGE=C LC_ALL=C apt-get -y install foo"] = []remoteexec.Result{{RC: 0}}
	conn := &queueConn{on: on}
	res, err := moduleAptRpm(context.Background(), conn, map[string]any{"package": "foo"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleAptRpmInstallAlreadyPresent(t *testing.T) {
	on := aptRpmToolsPresent()
	on["rpm -q --provides foo >/dev/null 2>&1"] = []remoteexec.Result{{RC: 0}}
	conn := &queueConn{on: on}
	res, err := moduleAptRpm(context.Background(), conn, map[string]any{"package": "foo"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("res = %+v, want unchanged", res)
	}
	for _, c := range conn.Commands {
		if c == "env LANGUAGE=C LC_ALL=C apt-get -y install foo" {
			t.Fatalf("did not expect an install call: %v", conn.Commands)
		}
	}
}

func TestModuleAptRpmInstallVerifyFails(t *testing.T) {
	on := aptRpmToolsPresent()
	on["rpm -q --provides foo >/dev/null 2>&1"] = []remoteexec.Result{{RC: 1}, {RC: 1}}
	on["env LANGUAGE=C LC_ALL=C apt-get -y install foo"] = []remoteexec.Result{{RC: 0}}
	conn := &queueConn{on: on}
	res, err := moduleAptRpm(context.Background(), conn, map[string]any{"package": "foo"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed when the package still isn't provided after install")
	}
}

func TestModuleAptRpmLatestUpgradeCheck(t *testing.T) {
	on := aptRpmToolsPresent()
	on["rpm -q --provides foo >/dev/null 2>&1"] = []remoteexec.Result{{RC: 0}}
	on["env LANGUAGE=C LC_ALL=C apt-cache policy foo"] = []remoteexec.Result{{RC: 0, Stdout: "foo:\n  Installed: 1.0\n  Candidate: 2.0\n"}}
	on["env LANGUAGE=C LC_ALL=C apt-get -y install foo"] = []remoteexec.Result{{RC: 0}}
	conn := &queueConn{on: on}
	res, err := moduleAptRpm(context.Background(), conn, map[string]any{"package": "foo", "state": "latest"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed: installed (1.0) < candidate (2.0)")
	}
}

func TestModuleAptRpmRemoveBasic(t *testing.T) {
	on := aptRpmToolsPresent()
	on["rpm -q foo >/dev/null 2>&1"] = []remoteexec.Result{{RC: 0}}
	on["env LANGUAGE=C LC_ALL=C apt-get -y remove foo"] = []remoteexec.Result{{RC: 0}}
	conn := &queueConn{on: on}
	res, err := moduleAptRpm(context.Background(), conn, map[string]any{"package": "foo", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleAptRpmRemoveAlreadyAbsent(t *testing.T) {
	on := aptRpmToolsPresent()
	on["rpm -q foo >/dev/null 2>&1"] = []remoteexec.Result{{RC: 1}}
	conn := &queueConn{on: on}
	res, err := moduleAptRpm(context.Background(), conn, map[string]any{"package": "foo", "state": "removed"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("res = %+v, want unchanged", res)
	}
}

func TestModuleAptRpmDistUpgradeUnchanged(t *testing.T) {
	on := aptRpmToolsPresent()
	on["env LANGUAGE=C LC_ALL=C apt-get -y dist-upgrade"] = []remoteexec.Result{
		{RC: 0, Stdout: "Reading package lists...\n0 upgraded, 0 newly installed, 0 removed.\n"},
	}
	conn := &queueConn{on: on}
	res, err := moduleAptRpm(context.Background(), conn, map[string]any{"dist_upgrade": true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("res = %+v, want unchanged", res)
	}
}

func TestModuleAptRpmDistUpgradeChanged(t *testing.T) {
	on := aptRpmToolsPresent()
	on["env LANGUAGE=C LC_ALL=C apt-get -y dist-upgrade"] = []remoteexec.Result{
		{RC: 0, Stdout: "1 upgraded, 0 newly installed, 0 removed.\n"},
	}
	conn := &queueConn{on: on}
	res, err := moduleAptRpm(context.Background(), conn, map[string]any{"dist_upgrade": true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleAptRpmUpdateKernelNoneAvailable(t *testing.T) {
	on := aptRpmToolsPresent()
	on["env LANGUAGE=C LC_ALL=C /usr/sbin/update-kernel -y"] = []remoteexec.Result{
		{RC: 1, Stderr: "There are no available kernels to install"},
	}
	conn := &queueConn{on: on}
	res, err := moduleAptRpm(context.Background(), conn, map[string]any{"update_kernel": true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v, want no failure for 'no available kernels'", res)
	}
	if res.Changed {
		t.Fatalf("res = %+v, want unchanged", res)
	}
}

func TestModuleAptRpmUpdateKernelError(t *testing.T) {
	on := aptRpmToolsPresent()
	on["env LANGUAGE=C LC_ALL=C /usr/sbin/update-kernel -y"] = []remoteexec.Result{
		{RC: 1, Stderr: "disk full"},
	}
	conn := &queueConn{on: on}
	res, err := moduleAptRpm(context.Background(), conn, map[string]any{"update_kernel": true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed for a genuine update-kernel error")
	}
}

func TestModuleAptRpmClean(t *testing.T) {
	on := aptRpmToolsPresent()
	on["du -sb /var/cache/apt/archives/ 2>/dev/null | cut -f1"] = []remoteexec.Result{
		{RC: 0, Stdout: "1000\n"}, {RC: 0, Stdout: "0\n"},
	}
	on["apt-get clean"] = []remoteexec.Result{{RC: 0}}
	conn := &queueConn{on: on}
	res, err := moduleAptRpm(context.Background(), conn, map[string]any{"clean": true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed: cache size shrank")
	}
}

func TestModuleAptRpmMissingTools(t *testing.T) {
	conn := &queueConn{on: map[string][]remoteexec.Result{
		"test -e /usr/bin/apt-get": {{RC: 1}},
	}}
	res, err := moduleAptRpm(context.Background(), conn, map[string]any{"package": "foo"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed when apt-get/rpm are missing")
	}
}

func TestModuleAptRpmBadState(t *testing.T) {
	conn := &queueConn{}
	if _, err := moduleAptRpm(context.Background(), conn, map[string]any{"state": "bogus"}); err == nil {
		t.Fatal("want error for invalid state")
	}
}

func TestModuleAptRpmLocalFile(t *testing.T) {
	on := aptRpmToolsPresent()
	on["rpm -qp --queryformat '%{NAME}' /tmp/foo-1.0.rpm"] = []remoteexec.Result{{RC: 0, Stdout: "foo\n"}}
	on["rpm -q --provides foo >/dev/null 2>&1"] = []remoteexec.Result{{RC: 1}, {RC: 0}}
	on["env LANGUAGE=C LC_ALL=C apt-get -y install /tmp/foo-1.0.rpm"] = []remoteexec.Result{{RC: 0}}
	conn := &queueConn{on: on}
	res, err := moduleAptRpm(context.Background(), conn, map[string]any{"package": "/tmp/foo-1.0.rpm"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}
