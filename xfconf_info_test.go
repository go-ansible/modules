package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleXfconfInfoListChannels(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"xfconf-query --version": {RC: 0, Stdout: "xfconf-query 4.18.1\n"},
		"xfconf-query --list":    {RC: 0, Stdout: "xfce4-desktop\ndisplays\nxsettings\n"},
	})
	res, err := moduleXfconfInfo(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	channels, ok := res.Extra["channels"].([]string)
	if !ok || len(channels) != 3 || channels[0] != "xfce4-desktop" {
		t.Fatalf("channels = %#v", res.Extra["channels"])
	}
}

func TestModuleXfconfInfoListProperties(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"xfconf-query --version":                  {RC: 0, Stdout: "xfconf-query 4.18.1\n"},
		"xfconf-query --channel xsettings --list": {RC: 0, Stdout: "/Xft/DPI\n/Xft/Hinting\n"},
	})
	res, err := moduleXfconfInfo(context.Background(), conn, map[string]any{"channel": "xsettings"})
	if err != nil {
		t.Fatal(err)
	}
	properties, ok := res.Extra["properties"].([]string)
	if !ok || len(properties) != 2 {
		t.Fatalf("properties = %#v", res.Extra["properties"])
	}
}

func TestModuleXfconfInfoScalarValue(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"xfconf-query --version":                               {RC: 0, Stdout: "xfconf-query 4.18.1\n"},
		"xfconf-query --channel xsettings --property /Xft/DPI": {RC: 0, Stdout: "96\n"},
	})
	res, err := moduleXfconfInfo(context.Background(), conn, map[string]any{
		"channel": "xsettings", "property": "/Xft/DPI",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Extra["value"] != "96" || res.Extra["is_array"] != false {
		t.Fatalf("res.Extra = %+v", res.Extra)
	}
}

func TestModuleXfconfInfoArrayValue(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"xfconf-query --version": {RC: 0, Stdout: "xfconf-query 4.18.1\n"},
		"xfconf-query --channel xfwm4 --property /general/workspace_names": {
			RC:     0,
			Stdout: "Value is an array with 3 items:\n\nMain\nWork\nTmp\n",
		},
	})
	res, err := moduleXfconfInfo(context.Background(), conn, map[string]any{
		"channel": "xfwm4", "property": "/general/workspace_names",
	})
	if err != nil {
		t.Fatal(err)
	}
	arr, ok := res.Extra["value_array"].([]string)
	if !ok || len(arr) != 3 || arr[2] != "Tmp" || res.Extra["is_array"] != true {
		t.Fatalf("res.Extra = %+v", res.Extra)
	}
}

func TestModuleXfconfInfoPropertyMissingChannel(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleXfconfInfo(context.Background(), conn, map[string]any{"property": "/x"}); err == nil {
		t.Fatal("want error for property without channel")
	}
}

func TestModuleXfconfInfoPropertyNotFound(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"xfconf-query --version":                       {RC: 0, Stdout: "xfconf-query 4.18.1\n"},
		"xfconf-query --channel c --property /missing": {RC: 1},
	})
	res, err := moduleXfconfInfo(context.Background(), conn, map[string]any{"channel": "c", "property": "/missing"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed for a missing property")
	}
}
