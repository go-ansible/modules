package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleJenkinsBuildInfoParsesJSON(t *testing.T) {
	conn := newJenkinsFakeConn(map[string]remoteexec.Result{
		"java -jar /tmp/jenkins-cli.jar -s http://localhost:8080 groovy =": {
			RC: 0, Stdout: `{"result":"SUCCESS","building":false,"number":3,"timestamp":123,"duration":456,"url":"http://x/job/my-job/3/"}` + "\n",
		},
	})
	args := map[string]any{"name": "my-job", "build_number": 3}
	res, err := moduleJenkinsBuildInfo(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	info, ok := res.Extra["build_info"].(map[string]any)
	if !ok || info["result"] != "SUCCESS" {
		t.Fatalf("build_info = %+v", res.Extra["build_info"])
	}
}

func TestModuleJenkinsBuildInfoNoJob(t *testing.T) {
	conn := newJenkinsFakeConn(map[string]remoteexec.Result{
		"java -jar /tmp/jenkins-cli.jar -s http://localhost:8080 groovy =": {RC: 0, Stdout: "NOJOB\n"},
	})
	args := map[string]any{"name": "missing-job"}
	res, err := moduleJenkinsBuildInfo(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed: no such job")
	}
}
