package modules

import (
	"context"
	"io"
	"strings"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

// jenkinsFakeConn is a fakeConn variant whose "download jenkins-cli.jar"
// command always succeeds without needing an explicit map entry per
// test (every jenkins_* test in this package would otherwise need to
// repeat the same curl-download boilerplate for every distinct URL).
type jenkinsFakeConn struct {
	*fakeConn
}

func newJenkinsFakeConn(on map[string]remoteexec.Result) *jenkinsFakeConn {
	return &jenkinsFakeConn{fakeConn: newFakeConn(on)}
}

func (f *jenkinsFakeConn) Exec(ctx context.Context, cmd string, stdin io.Reader) (remoteexec.Result, error) {
	if strings.HasPrefix(cmd, "curl -sSfL ") && strings.Contains(cmd, "/jnlpJars/jenkins-cli.jar -o ") {
		return remoteexec.Result{RC: 0}, nil
	}
	return f.fakeConn.Exec(ctx, cmd, stdin)
}

var _ remoteexec.Connection = (*jenkinsFakeConn)(nil)

func TestModuleJenkinsJobCreate(t *testing.T) {
	conn := newJenkinsFakeConn(map[string]remoteexec.Result{
		"java -jar /tmp/jenkins-cli.jar -s http://localhost:8080 get-job my-job":    {RC: 1, Stderr: "No such job 'my-job'"},
		"java -jar /tmp/jenkins-cli.jar -s http://localhost:8080 create-job my-job": {RC: 0},
	})
	args := map[string]any{"name": "my-job", "config": "<project></project>"}
	res, err := moduleJenkinsJob(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleJenkinsJobIdempotent(t *testing.T) {
	conn := newJenkinsFakeConn(map[string]remoteexec.Result{
		"java -jar /tmp/jenkins-cli.jar -s http://localhost:8080 get-job my-job": {RC: 0, Stdout: "<project></project>"},
	})
	args := map[string]any{"name": "my-job", "config": "<project></project>"}
	res, err := moduleJenkinsJob(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleJenkinsJobAbsentAlreadyGone(t *testing.T) {
	conn := newJenkinsFakeConn(map[string]remoteexec.Result{
		"java -jar /tmp/jenkins-cli.jar -s http://localhost:8080 get-job my-job": {RC: 1, Stderr: "No such job 'my-job'"},
	})
	args := map[string]any{"name": "my-job", "state": "absent"}
	res, err := moduleJenkinsJob(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleJenkinsJobMissingRuntime(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v java": {RC: 1},
	})
	args := map[string]any{"name": "my-job", "state": "absent"}
	res, err := moduleJenkinsJob(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed: java missing")
	}
}
