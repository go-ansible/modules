package modules

import (
	"context"
	"reflect"
	"testing"
)

// TestDebugKeepsTheTypeOfItsMessage pins what real ansible-core 2.21.4
// prints for a debug: msg: of each YAML type. A non-string msg used to
// be stringified through Go's %v, which is not a type change but
// MANGLING: a list came out "[1 2]" against real's [1, 2], and a
// mapping "map[a:1]" against {"a": 1}.
func TestDebugKeepsTheTypeOfItsMessage(t *testing.T) {
	tests := []struct {
		name string
		msg  any
		want any // nil means "carried in Msg only, as a string"
	}{
		{"int", 42, 42},
		{"float", 1.5, 1.5},
		{"bool", true, true},
		{"list", []any{1, 2}, []any{1, 2}},
		{"map", map[string]any{"a": 1}, map[string]any{"a": 1}},
		// A string needs no Extra: Msg already carries it faithfully.
		{"string", "hello", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := moduleDebug(context.Background(), nil, map[string]any{"msg": tt.msg})
			if err != nil {
				t.Fatal(err)
			}
			got, present := res.Extra["msg"]
			if tt.want == nil {
				if present {
					t.Errorf("a string msg needs no typed Extra, got %#v", got)
				}
				if res.Msg != "hello" {
					t.Errorf("Msg = %q", res.Msg)
				}
				return
			}
			if !present {
				t.Fatalf("msg of type %T must be carried typed in Extra", tt.msg)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Extra[msg] = %#v (%T), want %#v", got, got, tt.want)
			}
		})
	}
}

func TestDebugDefaultMessage(t *testing.T) {
	res, err := moduleDebug(context.Background(), nil, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Msg != "Hello world!" {
		t.Errorf("default msg = %q, want %q", res.Msg, "Hello world!")
	}
	if _, present := res.Extra["msg"]; present {
		t.Error("the default is a string and needs no typed Extra")
	}
}
