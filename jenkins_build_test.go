package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleJenkinsBuildTriggersAndWaits(t *testing.T) {
	conn := newJenkinsFakeConn(map[string]remoteexec.Result{
		"java -jar /tmp/jenkins-cli.jar -s http://localhost:8080 build my-job -s": {RC: 0},
	})
	args := map[string]any{"name": "my-job"}
	res, err := moduleJenkinsBuild(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleJenkinsBuildDetach(t *testing.T) {
	conn := newJenkinsFakeConn(map[string]remoteexec.Result{
		"java -jar /tmp/jenkins-cli.jar -s http://localhost:8080 build my-job": {RC: 0},
	})
	args := map[string]any{"name": "my-job", "detach": true}
	res, err := moduleJenkinsBuild(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleJenkinsBuildAbsentFailsLoud(t *testing.T) {
	conn := newJenkinsFakeConn(nil)
	args := map[string]any{"name": "my-job", "state": "absent"}
	res, err := moduleJenkinsBuild(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed: state=absent has no CLI equivalent")
	}
}

func TestModuleJenkinsBuildFailure(t *testing.T) {
	conn := newJenkinsFakeConn(map[string]remoteexec.Result{
		"java -jar /tmp/jenkins-cli.jar -s http://localhost:8080 build my-job -s": {RC: 1, Stderr: "build failed"},
	})
	args := map[string]any{"name": "my-job"}
	res, err := moduleJenkinsBuild(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed: build failed")
	}
}
