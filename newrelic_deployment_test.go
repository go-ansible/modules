package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleNewrelicDeploymentByApplicationID(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v newrelic": {RC: 0},
		"NEW_RELIC_API_KEY=tok newrelic apm deployment create --applicationId 12345 --revision abc123 --format json": {RC: 0},
	})
	res, err := moduleNewrelicDeployment(context.Background(), conn, map[string]any{
		"token": "tok", "application_id": "12345", "revision": "abc123",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}

func TestModuleNewrelicDeploymentByAppName(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v newrelic": {RC: 0},
		"NEW_RELIC_API_KEY=tok newrelic apm application search --name myapp --format json":                     {RC: 0, Stdout: `[{"name":"myapp","applicationId":42}]`},
		"NEW_RELIC_API_KEY=tok newrelic apm deployment create --applicationId 42 --revision 1.0 --format json": {RC: 0},
	})
	res, err := moduleNewrelicDeployment(context.Background(), conn, map[string]any{
		"token": "tok", "app_name": "myapp", "revision": "1.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
	if res.Extra["application_id"] != "42" {
		t.Fatalf("application_id = %v", res.Extra["application_id"])
	}
}

func TestModuleNewrelicDeploymentExactMatch(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v newrelic": {RC: 0},
		"NEW_RELIC_API_KEY=tok newrelic apm application search --name myapp --format json":                    {RC: 0, Stdout: `[{"name":"myapp-old","applicationId":1},{"name":"myapp","applicationId":2}]`},
		"NEW_RELIC_API_KEY=tok newrelic apm deployment create --applicationId 2 --revision 1.0 --format json": {RC: 0},
	})
	res, err := moduleNewrelicDeployment(context.Background(), conn, map[string]any{
		"token": "tok", "app_name": "myapp", "revision": "1.0", "app_name_exact_match": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
	if res.Extra["application_id"] != "2" {
		t.Fatalf("application_id = %v", res.Extra["application_id"])
	}
}

func TestModuleNewrelicDeploymentExactMatchNotFound(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v newrelic": {RC: 0},
		"NEW_RELIC_API_KEY=tok newrelic apm application search --name myapp --format json": {RC: 0, Stdout: `[{"name":"myapp-old","applicationId":1}]`},
	})
	res, err := moduleNewrelicDeployment(context.Background(), conn, map[string]any{
		"token": "tok", "app_name": "myapp", "revision": "1.0", "app_name_exact_match": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want failed, res = %+v", res)
	}
}

func TestModuleNewrelicDeploymentBothAppNameAndID(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v newrelic": {RC: 0},
	})
	res, err := moduleNewrelicDeployment(context.Background(), conn, map[string]any{
		"token": "tok", "app_name": "myapp", "application_id": "1", "revision": "1.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want failed, res = %+v", res)
	}
}

func TestModuleNewrelicDeploymentMissingBoth(t *testing.T) {
	conn := newFakeConn(nil)
	_, err := moduleNewrelicDeployment(context.Background(), conn, map[string]any{"token": "tok", "revision": "1.0"})
	if err == nil {
		t.Fatal("want error when neither app_name nor application_id is given")
	}
}

func TestModuleNewrelicDeploymentMissingBinary(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v newrelic": {RC: 1},
	})
	res, err := moduleNewrelicDeployment(context.Background(), conn, map[string]any{
		"token": "tok", "application_id": "1", "revision": "1.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want failed, res = %+v", res)
	}
}
