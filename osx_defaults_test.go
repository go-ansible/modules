package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleOsxDefaultsWriteStringWhenUnset(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v defaults": {RC: 0},
		"defaults read-type NSGlobalDomain AppleMeasurementUnits":                 {RC: 1},
		"defaults write NSGlobalDomain AppleMeasurementUnits -string Centimeters": {RC: 0},
	})
	res, err := moduleOsxDefaults(context.Background(), conn, map[string]any{
		"key": "AppleMeasurementUnits", "type": "string", "value": "Centimeters",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v, commands = %v", res, conn.Commands)
	}
}

func TestModuleOsxDefaultsStringUnchanged(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v defaults": {RC: 0},
		"defaults read-type NSGlobalDomain AppleMeasurementUnits": {RC: 0, Stdout: "Type is string\n"},
		"defaults read NSGlobalDomain AppleMeasurementUnits":      {RC: 0, Stdout: "Centimeters\n"},
	})
	res, err := moduleOsxDefaults(context.Background(), conn, map[string]any{
		"key": "AppleMeasurementUnits", "type": "string", "value": "Centimeters",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleOsxDefaultsBoolWrite(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v defaults": {RC: 0},
		"defaults read-type com.apple.Safari IncludeInternalDebugMenu":        {RC: 1},
		"defaults write com.apple.Safari IncludeInternalDebugMenu -bool TRUE": {RC: 0},
	})
	res, err := moduleOsxDefaults(context.Background(), conn, map[string]any{
		"domain": "com.apple.Safari", "key": "IncludeInternalDebugMenu", "type": "bool", "value": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v, commands = %v", res, conn.Commands)
	}
}

func TestModuleOsxDefaultsArrayUnchangedRegardlessOfOrder(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v defaults":                              {RC: 0},
		"defaults read-type NSGlobalDomain AppleLanguages": {RC: 0, Stdout: "Type is array\n"},
		"defaults read NSGlobalDomain AppleLanguages":      {RC: 0, Stdout: "(\n    \"nl\",\n    \"en\"\n)\n"},
	})
	res, err := moduleOsxDefaults(context.Background(), conn, map[string]any{
		"key": "AppleLanguages", "type": "array", "value": []any{"en", "nl"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleOsxDefaultsArrayAddOnlyNewElements(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v defaults":                                        {RC: 0},
		"defaults read-type NSGlobalDomain AppleLanguages":           {RC: 0, Stdout: "Type is array\n"},
		"defaults read NSGlobalDomain AppleLanguages":                {RC: 0, Stdout: "(\n    \"en\"\n)\n"},
		"defaults write NSGlobalDomain AppleLanguages -array-add nl": {RC: 0},
	})
	res, err := moduleOsxDefaults(context.Background(), conn, map[string]any{
		"key": "AppleLanguages", "type": "array", "value": []any{"en", "nl"}, "array_add": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v, commands = %v", res, conn.Commands)
	}
}

func TestModuleOsxDefaultsTypeMismatch(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v defaults": {RC: 0},
		"defaults read-type NSGlobalDomain AppleMeasurementUnits": {RC: 0, Stdout: "Type is string\n"},
		"defaults read NSGlobalDomain AppleMeasurementUnits":      {RC: 0, Stdout: "Centimeters\n"},
	})
	res, err := moduleOsxDefaults(context.Background(), conn, map[string]any{
		"key": "AppleMeasurementUnits", "type": "int", "value": 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed for a type mismatch")
	}
}

func TestModuleOsxDefaultsAbsentAlreadyUnset(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v defaults": {RC: 0},
		"defaults read-type com.geekchimp.macable ExampleKeyToRemove": {RC: 1},
	})
	res, err := moduleOsxDefaults(context.Background(), conn, map[string]any{
		"domain": "com.geekchimp.macable", "key": "ExampleKeyToRemove", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleOsxDefaultsAbsentDeletes(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v defaults": {RC: 0},
		"defaults read-type com.geekchimp.macable ExampleKeyToRemove": {RC: 0, Stdout: "Type is string\n"},
		"defaults read com.geekchimp.macable ExampleKeyToRemove":      {RC: 0, Stdout: "x\n"},
		"defaults delete com.geekchimp.macable ExampleKeyToRemove":    {RC: 0},
	})
	res, err := moduleOsxDefaults(context.Background(), conn, map[string]any{
		"domain": "com.geekchimp.macable", "key": "ExampleKeyToRemove", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v, commands = %v", res, conn.Commands)
	}
}

func TestModuleOsxDefaultsListState(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v defaults": {RC: 0},
		"defaults read-type NSGlobalDomain AppleMeasurementUnits": {RC: 0, Stdout: "Type is string\n"},
		"defaults read NSGlobalDomain AppleMeasurementUnits":      {RC: 0, Stdout: "Centimeters\n"},
	})
	res, err := moduleOsxDefaults(context.Background(), conn, map[string]any{
		"key": "AppleMeasurementUnits", "state": "list",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["value"] != "Centimeters" {
		t.Fatalf("value = %v", res.Extra["value"])
	}
}

func TestModuleOsxDefaultsListStateUnset(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v defaults":                            {RC: 0},
		"defaults read-type NSGlobalDomain DoesNotExist": {RC: 1},
	})
	res, err := moduleOsxDefaults(context.Background(), conn, map[string]any{
		"key": "DoesNotExist", "state": "list",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Extra["value"] != nil {
		t.Fatalf("value = %v, want nil", res.Extra["value"])
	}
}

func TestModuleOsxDefaultsDictWrite(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v defaults": {RC: 0},
		"command -v plutil":   {RC: 0},
		"defaults read-type com.apple.finder FXInfoPanesExpanded": {RC: 1},
	})
	res, err := moduleOsxDefaults(context.Background(), conn, map[string]any{
		"domain": "com.apple.finder", "key": "FXInfoPanesExpanded", "type": "dict",
		"value": map[string]any{"General": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v, commands = %v", res, conn.Commands)
	}
	found := false
	for _, c := range conn.Commands {
		if c == "defaults write com.apple.finder FXInfoPanesExpanded -dict General -bool TRUE" {
			found = true
		}
	}
	if !found {
		t.Fatalf("commands = %v", conn.Commands)
	}
}

func TestModuleOsxDefaultsNotInstalled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v defaults": {RC: 1},
	})
	res, err := moduleOsxDefaults(context.Background(), conn, map[string]any{
		"key": "x", "state": "list",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed when defaults is not on the target")
	}
}

func TestModuleOsxDefaultsDateNotSupported(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v defaults":                   {RC: 0},
		"defaults read-type NSGlobalDomain foo": {RC: 1},
	})
	res, err := moduleOsxDefaults(context.Background(), conn, map[string]any{
		"key": "foo", "type": "date", "value": "2024-01-01 00:00:00",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed for type=date (not implemented)")
	}
}

func TestModuleOsxDefaultsMissingKey(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleOsxDefaults(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing key")
	}
}
