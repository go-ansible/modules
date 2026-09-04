package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleAlertaCustomerCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v alerta": {RC: 0},
		"alerta --json --endpoint-url https://alerta.example.com customers": {
			RC: 0, Stdout: `[]`,
		},
		"alerta --json --endpoint-url https://alerta.example.com customer --customer Developer --org dev@example.com": {
			RC: 0, Stdout: `{"id":"abc123","customer":"Developer","match":"dev@example.com"}`,
		},
	})
	res, err := moduleAlertaCustomer(context.Background(), conn, map[string]any{
		"alerta_url": "https://alerta.example.com",
		"customer":   "Developer",
		"match":      "dev@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if !res.Changed {
		t.Fatal("want Changed=true")
	}
}

func TestModuleAlertaCustomerCreateAlreadyExists(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v alerta": {RC: 0},
		"alerta --json --endpoint-url https://alerta.example.com customers": {
			RC: 0, Stdout: `[{"id":"abc123","customer":"Developer","match":"dev@example.com"}]`,
		},
	})
	res, err := moduleAlertaCustomer(context.Background(), conn, map[string]any{
		"alerta_url": "https://alerta.example.com",
		"customer":   "Developer",
		"match":      "dev@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleAlertaCustomerDelete(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v alerta": {RC: 0},
		"alerta --json --endpoint-url https://alerta.example.com customers": {
			RC: 0, Stdout: `[{"id":"abc123","customer":"Developer","match":"dev@example.com"}]`,
		},
		"alerta --json --endpoint-url https://alerta.example.com customer --delete abc123": {RC: 0},
	})
	res, err := moduleAlertaCustomer(context.Background(), conn, map[string]any{
		"alerta_url": "https://alerta.example.com",
		"customer":   "Developer",
		"match":      "dev@example.com",
		"state":      "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleAlertaCustomerDeleteNotFound(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v alerta": {RC: 0},
		"alerta --json --endpoint-url https://alerta.example.com customers": {
			RC: 0, Stdout: `[]`,
		},
	})
	res, err := moduleAlertaCustomer(context.Background(), conn, map[string]any{
		"alerta_url": "https://alerta.example.com",
		"customer":   "Developer",
		"match":      "dev@example.com",
		"state":      "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleAlertaCustomerMissingArgs(t *testing.T) {
	conn := newFakeConn(nil)
	_, err := moduleAlertaCustomer(context.Background(), conn, map[string]any{})
	if err == nil {
		t.Fatal("want error for missing required arguments")
	}
}

func TestModuleAlertaCustomerAPIKeyOverEnv(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v alerta": {RC: 0},
		"ALERTA_API_KEY=secretkey alerta --json --endpoint-url https://alerta.example.com customers": {
			RC: 0, Stdout: `[]`,
		},
		"ALERTA_API_KEY=secretkey alerta --json --endpoint-url https://alerta.example.com customer --customer Developer --org dev@example.com": {
			RC: 0, Stdout: `{}`,
		},
	})
	res, err := moduleAlertaCustomer(context.Background(), conn, map[string]any{
		"alerta_url": "https://alerta.example.com",
		"customer":   "Developer",
		"match":      "dev@example.com",
		"api_key":    "secretkey",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	for _, c := range conn.Commands {
		if c == "" {
			t.Fatal("empty command recorded")
		}
	}
}
