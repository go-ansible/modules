package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleJenkinsPluginInstallsFresh(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e /var/lib/jenkins/plugins/git.jpi":                                                       {RC: 1},
		"curl -sSfL https://updates.jenkins.io/latest/git.hpi -o /tmp/git.jpi.download":                  {RC: 0},
		"mkdir -p /var/lib/jenkins/plugins && mv /tmp/git.jpi.download /var/lib/jenkins/plugins/git.jpi": {RC: 0},
	})
	res, err := moduleJenkinsPlugin(context.Background(), conn, map[string]any{"name": "git"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleJenkinsPluginAlreadyPresent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e /var/lib/jenkins/plugins/git.jpi": {RC: 0},
	})
	res, err := moduleJenkinsPlugin(context.Background(), conn, map[string]any{"name": "git"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleJenkinsPluginPin(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e /var/lib/jenkins/plugins/git.jpi":        {RC: 0},
		"test -e /var/lib/jenkins/plugins/git.jpi.pinned": {RC: 1},
		"touch /var/lib/jenkins/plugins/git.jpi.pinned":   {RC: 0},
	})
	args := map[string]any{"name": "git", "state": "pinned"}
	res, err := moduleJenkinsPlugin(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleJenkinsPluginAbsent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e /var/lib/jenkins/plugins/git.jpi": {RC: 0},
		"rm -f /var/lib/jenkins/plugins/git.jpi /var/lib/jenkins/plugins/git.jpi.pinned /var/lib/jenkins/plugins/git.jpi.disabled": {RC: 0},
	})
	args := map[string]any{"name": "git", "state": "absent"}
	res, err := moduleJenkinsPlugin(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}
