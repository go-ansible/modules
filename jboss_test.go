package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleJbossRedeployDifferentContent(t *testing.T) {
	old := jbossPollInterval
	jbossPollInterval = 0
	defer func() { jbossPollInterval = old }()

	fc := newFakeConn(map[string]remoteexec.Result{
		"test -e /opt/wildfly/deployments":                             {RC: 0},
		"test -e /tmp/hello-1.1.war":                                   {RC: 0},
		"test -e /opt/wildfly/deployments/hello.war.deployed":          {RC: 0},
		"cmp -s /tmp/hello-1.1.war /opt/wildfly/deployments/hello.war": {RC: 1},
		"rm -f /opt/wildfly/deployments/hello.war.deployed":            {RC: 0},
		"cp -p /tmp/hello-1.1.war /opt/wildfly/deployments/hello.war":  {RC: 0},
	})
	res, err := moduleJboss(context.Background(), fc, map[string]any{
		"src": "/tmp/hello-1.1.war", "deployment": "hello.war", "deploy_path": "/opt/wildfly/deployments",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleJbossAlreadyDeployedSame(t *testing.T) {
	fc := newFakeConn(map[string]remoteexec.Result{
		"test -e /opt/wildfly/deployments":                         {RC: 0},
		"test -e /tmp/hello.war":                                   {RC: 0},
		"test -e /opt/wildfly/deployments/hello.war.deployed":      {RC: 0},
		"cmp -s /tmp/hello.war /opt/wildfly/deployments/hello.war": {RC: 0},
	})
	res, err := moduleJboss(context.Background(), fc, map[string]any{
		"src": "/tmp/hello.war", "deployment": "hello.war", "deploy_path": "/opt/wildfly/deployments",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged when already deployed with identical content")
	}
}

func TestModuleJbossDeployTimesOut(t *testing.T) {
	oldInterval, oldAttempts := jbossPollInterval, jbossMaxPollAttempts
	jbossPollInterval = 0
	jbossMaxPollAttempts = 2
	defer func() { jbossPollInterval, jbossMaxPollAttempts = oldInterval, oldAttempts }()

	fc := newFakeConn(map[string]remoteexec.Result{
		"test -e /opt/wildfly/deployments":                        {RC: 0},
		"test -e /tmp/hello.war":                                  {RC: 0},
		"test -e /opt/wildfly/deployments/hello.war.deployed":     {RC: 1},
		"test -e /opt/wildfly/deployments/hello.war.failed":       {RC: 1},
		"cp -p /tmp/hello.war /opt/wildfly/deployments/hello.war": {RC: 0},
	})
	res, err := moduleJboss(context.Background(), fc, map[string]any{
		"src": "/tmp/hello.war", "deployment": "hello.war", "deploy_path": "/opt/wildfly/deployments",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed when the scanner never confirms the deploy within the poll budget")
	}
}

func TestModuleJbossDeployFailedMarker(t *testing.T) {
	old := jbossPollInterval
	jbossPollInterval = 0
	defer func() { jbossPollInterval = old }()

	fc := newFakeConn(map[string]remoteexec.Result{
		"test -e /opt/wildfly/deployments":                        {RC: 0},
		"test -e /tmp/hello.war":                                  {RC: 0},
		"test -e /opt/wildfly/deployments/hello.war.deployed":     {RC: 1},
		"test -e /opt/wildfly/deployments/hello.war.failed":       {RC: 0},
		"cp -p /tmp/hello.war /opt/wildfly/deployments/hello.war": {RC: 0},
	})
	res, err := moduleJboss(context.Background(), fc, map[string]any{
		"src": "/tmp/hello.war", "deployment": "hello.war", "deploy_path": "/opt/wildfly/deployments",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed when the scanner drops a .failed marker")
	}
}

func TestModuleJbossUndeploy(t *testing.T) {
	old := jbossPollInterval
	jbossPollInterval = 0
	defer func() { jbossPollInterval = old }()

	fc := newFakeConn(map[string]remoteexec.Result{
		"test -e /opt/wildfly/deployments":                      {RC: 0},
		"test -e /opt/wildfly/deployments/hello.war.deployed":   {RC: 0},
		"rm -f /opt/wildfly/deployments/hello.war.deployed":     {RC: 0},
		"test -e /opt/wildfly/deployments/hello.war.undeployed": {RC: 0},
	})
	res, err := moduleJboss(context.Background(), fc, map[string]any{
		"deployment": "hello.war", "deploy_path": "/opt/wildfly/deployments", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleJbossUndeployAlreadyAbsent(t *testing.T) {
	fc := newFakeConn(map[string]remoteexec.Result{
		"test -e /opt/wildfly/deployments":                    {RC: 0},
		"test -e /opt/wildfly/deployments/hello.war.deployed": {RC: 1},
	})
	res, err := moduleJboss(context.Background(), fc, map[string]any{
		"deployment": "hello.war", "deploy_path": "/opt/wildfly/deployments", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged when already undeployed")
	}
}

func TestModuleJbossDeployPathMissing(t *testing.T) {
	fc := newFakeConn(map[string]remoteexec.Result{
		"test -e /opt/wildfly/deployments": {RC: 1},
	})
	res, err := moduleJboss(context.Background(), fc, map[string]any{
		"deployment": "hello.war", "deploy_path": "/opt/wildfly/deployments", "src": "/tmp/hello.war",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed when deploy_path does not exist")
	}
}

func TestModuleJbossMissingSrc(t *testing.T) {
	fc := newFakeConn(nil)
	if _, err := moduleJboss(context.Background(), fc, map[string]any{"deployment": "hello.war"}); err == nil {
		t.Fatal("want error: src is required when state=present")
	}
}

func TestModuleJbossMissingDeployment(t *testing.T) {
	fc := newFakeConn(nil)
	if _, err := moduleJboss(context.Background(), fc, map[string]any{}); err == nil {
		t.Fatal("want error for missing deployment")
	}
}
