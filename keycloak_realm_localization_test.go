package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleKeycloakRealmLocalizationCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v kcadm.sh":                                       {RC: 0},
		"kcadm.sh get localization/en -r my-realm":                  {RC: 1},
		"kcadm.sh update localization/en/greeting -r my-realm -f -": {RC: 0},
	})
	args := map[string]any{
		"parent_id": "my-realm",
		"locale":    "en",
		"overrides": []any{map[string]any{"key": "greeting", "value": "Hello"}},
	}
	res, err := moduleKeycloakRealmLocalization(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleKeycloakRealmLocalizationIdempotent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v kcadm.sh": {RC: 0},
		"kcadm.sh get localization/en -r my-realm": {
			RC: 0, Stdout: `{"greeting":"Hello"}`,
		},
	})
	args := map[string]any{
		"parent_id": "my-realm",
		"locale":    "en",
		"overrides": []any{map[string]any{"key": "greeting", "value": "Hello"}},
	}
	res, err := moduleKeycloakRealmLocalization(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleKeycloakRealmLocalizationAbsentForceRemovesAll(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v kcadm.sh": {RC: 0},
		"kcadm.sh get localization/de -r my-realm": {
			RC: 0, Stdout: `{"greeting":"Hallo","farewell":"Tschuss"}`,
		},
		"kcadm.sh delete localization/de -r my-realm": {RC: 0},
	})
	args := map[string]any{
		"parent_id": "my-realm",
		"locale":    "de",
		"state":     "absent",
		"force":     true,
	}
	res, err := moduleKeycloakRealmLocalization(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleKeycloakRealmLocalizationAbsentListedOnly(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v kcadm.sh": {RC: 0},
		"kcadm.sh get localization/de -r my-realm": {
			RC: 0, Stdout: `{"app.title":"Meine App","foo":"bar"}`,
		},
		"kcadm.sh delete localization/de/app.title -r my-realm": {RC: 0},
	})
	args := map[string]any{
		"parent_id": "my-realm",
		"locale":    "de",
		"state":     "absent",
		"overrides": []any{map[string]any{"key": "app.title"}},
	}
	res, err := moduleKeycloakRealmLocalization(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}
