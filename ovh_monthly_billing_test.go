package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleOvhMonthlyBillingActivates(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v ovhcloud": {RC: 0},
		"ovhcloud cloud instance get inst1 --cloud-project proj1 -o json":                      {RC: 0, Stdout: `{"id":"inst1","monthlyBilling":null}`},
		"ovhcloud cloud instance activate-monthly-billing inst1 --cloud-project proj1 -o json": {RC: 0, Stdout: `{"monthlyBilling":{"status":"activationPending"}}`},
	})
	res, err := moduleOvhMonthlyBilling(context.Background(), conn, map[string]any{
		"project_id": "proj1", "instance_id": "inst1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}

func TestModuleOvhMonthlyBillingAlreadyEnabled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v ovhcloud": {RC: 0},
		"ovhcloud cloud instance get inst1 --cloud-project proj1 -o json": {RC: 0, Stdout: `{"id":"inst1","monthlyBilling":{"status":"ok"}}`},
	})
	res, err := moduleOvhMonthlyBilling(context.Background(), conn, map[string]any{
		"project_id": "proj1", "instance_id": "inst1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("want unchanged, res = %+v", res)
	}
}

func TestModuleOvhMonthlyBillingPendingIsUnchanged(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v ovhcloud": {RC: 0},
		"ovhcloud cloud instance get inst1 --cloud-project proj1 -o json": {RC: 0, Stdout: `{"id":"inst1","monthlyBilling":{"status":"activationPending"}}`},
	})
	res, err := moduleOvhMonthlyBilling(context.Background(), conn, map[string]any{
		"project_id": "proj1", "instance_id": "inst1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("want unchanged, res = %+v", res)
	}
}

func TestModuleOvhMonthlyBillingInstanceMissing(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v ovhcloud": {RC: 0},
		"ovhcloud cloud instance get inst1 --cloud-project proj1 -o json": {RC: 1, Stderr: "404"},
	})
	res, err := moduleOvhMonthlyBilling(context.Background(), conn, map[string]any{
		"project_id": "proj1", "instance_id": "inst1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want failed, res = %+v", res)
	}
}

func TestModuleOvhMonthlyBillingMissingArgs(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v ovhcloud": {RC: 0},
	})
	_, err := moduleOvhMonthlyBilling(context.Background(), conn, map[string]any{"project_id": "proj1"})
	if err == nil {
		t.Fatal("want error for missing instance_id")
	}
}
