package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleJenkinsCredentialCreateUserAndPass(t *testing.T) {
	conn := newJenkinsFakeConn(map[string]remoteexec.Result{
		"java -jar /tmp/jenkins-cli.jar -s http://localhost:8080 create-credentials-by-xml system::system::jenkins _": {RC: 0},
	})
	args := map[string]any{
		"type": "user_and_pass", "id": "cred-1", "username": "u", "password": "p",
	}
	res, err := moduleJenkinsCredential(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleJenkinsCredentialTokenTypeFailsLoud(t *testing.T) {
	conn := newJenkinsFakeConn(nil)
	args := map[string]any{"type": "token", "name": "tok"}
	res, err := moduleJenkinsCredential(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed: type=token unsupported")
	}
}

func TestModuleJenkinsCredentialAbsentDeletes(t *testing.T) {
	conn := newJenkinsFakeConn(map[string]remoteexec.Result{
		"java -jar /tmp/jenkins-cli.jar -s http://localhost:8080 delete-credentials system::system::jenkins _ cred-1": {RC: 0},
	})
	args := map[string]any{"type": "text", "id": "cred-1", "secret": "s", "state": "absent"}
	res, err := moduleJenkinsCredential(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}
