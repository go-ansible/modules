package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleJenkinsNodeCreate(t *testing.T) {
	conn := newJenkinsFakeConn(map[string]remoteexec.Result{
		"java -jar /tmp/jenkins-cli.jar -s http://localhost:8080 dump-node-config my-node": {RC: 1, Stderr: "No such node 'my-node'"},
		"java -jar /tmp/jenkins-cli.jar -s http://localhost:8080 create-node my-node":      {RC: 0},
	})
	args := map[string]any{"name": "my-node"}
	res, err := moduleJenkinsNode(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["created"] != true {
		t.Fatalf("created = %v", res.Extra["created"])
	}
}

func TestModuleJenkinsNodeDisableWithMessage(t *testing.T) {
	conn := newJenkinsFakeConn(map[string]remoteexec.Result{
		"java -jar /tmp/jenkins-cli.jar -s http://localhost:8080 dump-node-config my-node":        {RC: 0, Stdout: "<slave></slave>"},
		"java -jar /tmp/jenkins-cli.jar -s http://localhost:8080 offline-node my-node -m offline": {RC: 0},
	})
	args := map[string]any{"name": "my-node", "state": "disabled", "offline_message": "offline"}
	res, err := moduleJenkinsNode(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleJenkinsNodeOfflineMessageRequiresDisabled(t *testing.T) {
	conn := newJenkinsFakeConn(nil)
	args := map[string]any{"name": "my-node", "offline_message": "x"}
	_, err := moduleJenkinsNode(context.Background(), conn, args)
	if err == nil {
		t.Fatal("want error: offline_message requires state=disabled")
	}
}

func TestModuleJenkinsNodeAbsentAlreadyGone(t *testing.T) {
	conn := newJenkinsFakeConn(map[string]remoteexec.Result{
		"java -jar /tmp/jenkins-cli.jar -s http://localhost:8080 dump-node-config my-node": {RC: 1, Stderr: "No such node 'my-node'"},
	})
	args := map[string]any{"name": "my-node", "state": "absent"}
	res, err := moduleJenkinsNode(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}
