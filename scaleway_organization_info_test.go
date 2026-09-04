package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleScalewayOrganizationInfoConfigured(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v scw":                         {RC: 0},
		"scw config get default_organization_id": {RC: 0, Stdout: "org-123\n"},
	})
	res, err := moduleScalewayOrganizationInfo(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	orgs, ok := res.Extra["organizations"].([]map[string]any)
	if !ok || len(orgs) != 1 || orgs[0]["id"] != "org-123" {
		t.Fatalf("organizations = %+v", res.Extra["organizations"])
	}
}

func TestModuleScalewayOrganizationInfoUnset(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v scw":                         {RC: 0},
		"scw config get default_organization_id": {RC: 0, Stdout: ""},
	})
	res, err := moduleScalewayOrganizationInfo(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	orgs, ok := res.Extra["organizations"].([]map[string]any)
	if !ok || len(orgs) != 0 {
		t.Fatalf("organizations = %+v", res.Extra["organizations"])
	}
}
