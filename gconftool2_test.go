package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleGconftool2SetChanges(t *testing.T) {
	// The second --get (post-set) must return the new value; fakeConn
	// only supports one fixed result per exact command, so use a
	// sequential responder for this one repeated command.
	seq := &apache2SeqConn{on: map[string][]remoteexec.Result{
		"command -v gconftool-2": {{RC: 0}},
		"gconftool-2 --version":  {{RC: 0, Stdout: "3.2.6\n"}},
		"gconftool-2 --get /desktop/gnome/interface/font_name": {
			{RC: 0, Stdout: "Sans 10\n"},
			{RC: 0, Stdout: "Serif 12\n"},
		},
		"gconftool-2 --type string --set /desktop/gnome/interface/font_name 'Serif 12'": {{RC: 0}},
	}}
	res, err := moduleGconftool2(context.Background(), seq, map[string]any{
		"key": "/desktop/gnome/interface/font_name", "state": "present",
		"value": "Serif 12", "value_type": "string",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["previous_value"] != "Sans 10" || res.Extra["value"] != "Serif 12" {
		t.Fatalf("previous_value/value = %v/%v", res.Extra["previous_value"], res.Extra["value"])
	}
}

func TestModuleGconftool2SetUnchanged(t *testing.T) {
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v gconftool-2":                               {RC: 0},
		"gconftool-2 --version":                                {RC: 0, Stdout: "3.2.6\n"},
		"gconftool-2 --get /desktop/gnome/interface/font_name": {RC: 0, Stdout: "Serif 12\n"},
		"gconftool-2 --type string --set /desktop/gnome/interface/font_name 'Serif 12'": {RC: 0},
	})
	res, err := moduleGconftool2(context.Background(), fc, map[string]any{
		"key": "/desktop/gnome/interface/font_name", "state": "present",
		"value": "Serif 12", "value_type": "string",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged: --get returns the same value before and after --set")
	}
}

func TestModuleGconftool2Unset(t *testing.T) {
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v gconftool-2":                                 {RC: 0},
		"gconftool-2 --version":                                  {RC: 0, Stdout: "3.2.6\n"},
		"gconftool-2 --get /desktop/gnome/interface/font_name":   {RC: 0, Stdout: "Serif 12\n"},
		"gconftool-2 --unset /desktop/gnome/interface/font_name": {RC: 0},
	})
	res, err := moduleGconftool2(context.Background(), fc, map[string]any{
		"key": "/desktop/gnome/interface/font_name", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["value"] != nil {
		t.Fatalf("value = %v, want nil after unset", res.Extra["value"])
	}
}

func TestModuleGconftool2UnsetAlreadyAbsent(t *testing.T) {
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v gconftool-2":                                 {RC: 0},
		"gconftool-2 --version":                                  {RC: 0, Stdout: "3.2.6\n"},
		"gconftool-2 --get /desktop/gnome/interface/font_name":   {RC: 1},
		"gconftool-2 --unset /desktop/gnome/interface/font_name": {RC: 0},
	})
	res, err := moduleGconftool2(context.Background(), fc, map[string]any{
		"key": "/desktop/gnome/interface/font_name", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged when the key was already absent")
	}
}

func TestModuleGconftool2Direct(t *testing.T) {
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v gconftool-2":     {RC: 0},
		"gconftool-2 --version":      {RC: 0, Stdout: "3.2.6\n"},
		"gconftool-2 --get /foo/bar": {RC: 1},
		"gconftool-2 --direct --config-source xml:readwrite:/etc/gconf/gconf.xml.defaults --type int --set /foo/bar 42": {RC: 0},
	})
	res, err := moduleGconftool2(context.Background(), fc, map[string]any{
		"key": "/foo/bar", "state": "present", "value": "42", "value_type": "int",
		"direct": true, "config_source": "xml:readwrite:/etc/gconf/gconf.xml.defaults",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleGconftool2DirectRequiresConfigSource(t *testing.T) {
	fc := newFakeConn(nil)
	if _, err := moduleGconftool2(context.Background(), fc, map[string]any{
		"key": "/foo/bar", "state": "present", "value": "42", "value_type": "int", "direct": true,
	}); err == nil {
		t.Fatal("want error: direct requires config_source")
	}
}

func TestModuleGconftool2MissingValueForPresent(t *testing.T) {
	fc := newFakeConn(nil)
	if _, err := moduleGconftool2(context.Background(), fc, map[string]any{
		"key": "/foo/bar", "state": "present",
	}); err == nil {
		t.Fatal("want error: value/value_type required for state=present")
	}
}

func TestModuleGconftool2MissingArgs(t *testing.T) {
	fc := newFakeConn(nil)
	if _, err := moduleGconftool2(context.Background(), fc, map[string]any{"state": "present"}); err == nil {
		t.Fatal("want error for missing key")
	}
	if _, err := moduleGconftool2(context.Background(), fc, map[string]any{"key": "/foo"}); err == nil {
		t.Fatal("want error for missing state")
	}
}
