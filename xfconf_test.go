package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleXfconfSetScalar(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"xfconf-query --version":                                                             {RC: 0, Stdout: "xfconf-query 4.18.1\n"},
		"xfconf-query --channel xsettings --property /Xft/DPI":                               {RC: 1},
		"xfconf-query --channel xsettings --property /Xft/DPI --create --type int --set 192": {RC: 0},
	})
	res, err := moduleXfconf(context.Background(), conn, map[string]any{
		"channel": "xsettings", "property": "/Xft/DPI", "value_type": []any{"int"}, "value": []any{"192"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["value"] != "192" || res.Extra["version"] != "4.18.1" {
		t.Fatalf("res.Extra = %+v", res.Extra)
	}
}

func TestModuleXfconfSetArray(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"xfconf-query --version": {RC: 0, Stdout: "xfconf-query 4.18.1\n"},
		"xfconf-query --channel xfwm4 --property /general/workspace_names": {
			RC:     0,
			Stdout: "Value is an array with 2 items:\n\nOld1\nOld2\n",
		},
		"xfconf-query --channel xfwm4 --property /general/workspace_names --create --type string --set Main --type string --set Work1": {RC: 0},
	})
	res, err := moduleXfconf(context.Background(), conn, map[string]any{
		"channel": "xfwm4", "property": "/general/workspace_names",
		"value_type": []any{"string"}, "value": []any{"Main", "Work1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
	values, ok := res.Extra["value"].([]string)
	if !ok || len(values) != 2 || values[0] != "Main" {
		t.Fatalf("value = %#v", res.Extra["value"])
	}
	prev, ok := res.Extra["previous_value"].([]string)
	if !ok || len(prev) != 2 || prev[0] != "Old1" {
		t.Fatalf("previous_value = %#v", res.Extra["previous_value"])
	}
}

func TestModuleXfconfUnchanged(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"xfconf-query --version":                               {RC: 0, Stdout: "xfconf-query 4.18.1\n"},
		"xfconf-query --channel xsettings --property /Xft/DPI": {RC: 0, Stdout: "192\n"},
	})
	res, err := moduleXfconf(context.Background(), conn, map[string]any{
		"channel": "xsettings", "property": "/Xft/DPI", "value_type": []any{"int"}, "value": []any{"192"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleXfconfReset(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"xfconf-query --version":                                       {RC: 0, Stdout: "xfconf-query 4.18.1\n"},
		"xfconf-query --channel xsettings --property /Xft/DPI":         {RC: 0, Stdout: "192\n"},
		"xfconf-query --channel xsettings --property /Xft/DPI --reset": {RC: 0},
	})
	res, err := moduleXfconf(context.Background(), conn, map[string]any{
		"channel": "xsettings", "property": "/Xft/DPI", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Extra["value_type"] != "none" {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleXfconfResetAbsentNoop(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"xfconf-query --version":                               {RC: 0, Stdout: "xfconf-query 4.18.1\n"},
		"xfconf-query --channel xsettings --property /Xft/DPI": {RC: 1},
	})
	res, err := moduleXfconf(context.Background(), conn, map[string]any{
		"channel": "xsettings", "property": "/Xft/DPI", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleXfconfForceArray(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"xfconf-query --version": {RC: 0, Stdout: "xfconf-query 4.18.1\n"},
		"xfconf-query --channel xfwm4 --property /general/workspace_names":                                                 {RC: 1},
		"xfconf-query --channel xfwm4 --property /general/workspace_names --create --type string --set Main --force-array": {RC: 0},
	})
	res, err := moduleXfconf(context.Background(), conn, map[string]any{
		"channel": "xfwm4", "property": "/general/workspace_names",
		"value_type": []any{"string"}, "value": []any{"Main"}, "force_array": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleXfconfInferBoolType(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"xfconf-query --version":                                                 {RC: 0, Stdout: "xfconf-query 4.18.1\n"},
		"xfconf-query --channel c --property /p":                                 {RC: 1},
		"xfconf-query --channel c --property /p --create --type bool --set true": {RC: 0},
	})
	res, err := moduleXfconf(context.Background(), conn, map[string]any{
		"channel": "c", "property": "/p", "value": []any{true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleXfconfMissingValueOnPresent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"xfconf-query --version":                 {RC: 0, Stdout: "xfconf-query 4.18.1\n"},
		"xfconf-query --channel c --property /p": {RC: 1},
	})
	if _, err := moduleXfconf(context.Background(), conn, map[string]any{"channel": "c", "property": "/p"}); err == nil {
		t.Fatal("want error for missing value")
	}
}

func TestModuleXfconfMissingArgs(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleXfconf(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing channel")
	}
}
