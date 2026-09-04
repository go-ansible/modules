package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleJenkinsScriptRuns(t *testing.T) {
	conn := newJenkinsFakeConn(map[string]remoteexec.Result{
		"java -jar /tmp/jenkins-cli.jar -s http://localhost:8080 groovy =": {RC: 0, Stdout: "hello\n"},
	})
	args := map[string]any{"script": "println('hello')"}
	res, err := moduleJenkinsScript(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["output"] != "hello\n" {
		t.Fatalf("output = %v", res.Extra["output"])
	}
}

func TestModuleJenkinsScriptTemplating(t *testing.T) {
	conn := newJenkinsFakeConn(map[string]remoteexec.Result{
		"java -jar /tmp/jenkins-cli.jar -s http://localhost:8080 groovy =": {RC: 0, Stdout: "ok\n"},
	})
	args := map[string]any{
		"script": "println('${name}')",
		"args":   map[string]any{"name": "world"},
	}
	res, err := moduleJenkinsScript(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	sent := conn.Stdins[len(conn.Stdins)-1]
	if sent != "println('world')" {
		t.Fatalf("templated script = %q", sent)
	}
}

func TestModuleJenkinsScriptFails(t *testing.T) {
	conn := newJenkinsFakeConn(map[string]remoteexec.Result{
		"java -jar /tmp/jenkins-cli.jar -s http://localhost:8080 groovy =": {RC: 1, Stderr: "groovy.lang.MissingPropertyException"},
	})
	args := map[string]any{"script": "bogus"}
	res, err := moduleJenkinsScript(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed: script error")
	}
}
