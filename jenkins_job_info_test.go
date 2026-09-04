package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleJenkinsJobInfoListsAndFilters(t *testing.T) {
	conn := newJenkinsFakeConn(map[string]remoteexec.Result{
		"java -jar /tmp/jenkins-cli.jar -s http://localhost:8080 list-jobs":     {RC: 0, Stdout: "job-a\njob-b\n"},
		"java -jar /tmp/jenkins-cli.jar -s http://localhost:8080 get-job job-a": {RC: 0, Stdout: "<project></project>"},
	})
	args := map[string]any{"name": "job-a"}
	res, err := moduleJenkinsJobInfo(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	jobs, ok := res.Extra["jobs"].([]map[string]any)
	if !ok || len(jobs) != 1 || jobs[0]["name"] != "job-a" {
		t.Fatalf("jobs = %+v", res.Extra["jobs"])
	}
}

func TestModuleJenkinsJobInfoGlob(t *testing.T) {
	conn := newJenkinsFakeConn(map[string]remoteexec.Result{
		"java -jar /tmp/jenkins-cli.jar -s http://localhost:8080 list-jobs":     {RC: 0, Stdout: "foo-1\nfoo-2\nbar-1\n"},
		"java -jar /tmp/jenkins-cli.jar -s http://localhost:8080 get-job foo-1": {RC: 0, Stdout: "<project></project>"},
		"java -jar /tmp/jenkins-cli.jar -s http://localhost:8080 get-job foo-2": {RC: 0, Stdout: "<project></project>"},
	})
	args := map[string]any{"glob": "foo-*"}
	res, err := moduleJenkinsJobInfo(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	jobs, ok := res.Extra["jobs"].([]map[string]any)
	if !ok || len(jobs) != 2 {
		t.Fatalf("jobs = %+v", res.Extra["jobs"])
	}
}
