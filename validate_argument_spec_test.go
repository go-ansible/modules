package modules

import (
	"context"
	"testing"
)

func TestArgMatchesType(t *testing.T) {
	if !argMatchesType("x", "str") {
		t.Error("str should match string")
	}
	if argMatchesType(1, "str") {
		t.Error("int should not match str")
	}
	if !argMatchesType(1, "int") {
		t.Error("int should match int")
	}
	if !argMatchesType(1.5, "int") {
		t.Error("float64 should match int (loose)")
	}
	if !argMatchesType(true, "bool") {
		t.Error("bool should match bool")
	}
	if !argMatchesType([]any{"a"}, "list") {
		t.Error("[]any should match list")
	}
	if !argMatchesType(map[string]any{}, "dict") {
		t.Error("map should match dict")
	}
	if !argMatchesType("anything", "some_custom_type") {
		t.Error("unrecognized type should not fail closed")
	}
}

func TestChoiceContains(t *testing.T) {
	if !choiceContains([]any{"a", "b"}, "a") {
		t.Error("want contains")
	}
	if choiceContains([]any{"a", "b"}, "c") {
		t.Error("want not contains")
	}
}

func TestModuleValidateArgumentSpecPasses(t *testing.T) {
	conn := newFakeConn(nil)
	res, err := moduleValidateArgumentSpec(context.Background(), conn, map[string]any{
		"argument_spec": map[string]any{
			"name": map[string]any{"type": "str", "required": true},
			"port": map[string]any{"type": "int", "default": 80},
		},
		"provided_arguments": map[string]any{"name": "web"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleValidateArgumentSpecMissingRequired(t *testing.T) {
	conn := newFakeConn(nil)
	res, err := moduleValidateArgumentSpec(context.Background(), conn, map[string]any{
		"argument_spec": map[string]any{
			"name": map[string]any{"type": "str", "required": true},
		},
		"provided_arguments": map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed for missing required argument")
	}
}

func TestModuleValidateArgumentSpecWrongType(t *testing.T) {
	conn := newFakeConn(nil)
	res, err := moduleValidateArgumentSpec(context.Background(), conn, map[string]any{
		"argument_spec": map[string]any{
			"port": map[string]any{"type": "int"},
		},
		"provided_arguments": map[string]any{"port": "not-an-int"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed for wrong type")
	}
}

func TestModuleValidateArgumentSpecBadChoice(t *testing.T) {
	conn := newFakeConn(nil)
	res, err := moduleValidateArgumentSpec(context.Background(), conn, map[string]any{
		"argument_spec": map[string]any{
			"state": map[string]any{"type": "str", "choices": []any{"present", "absent"}},
		},
		"provided_arguments": map[string]any{"state": "bogus"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed for invalid choice")
	}
}

func TestModuleValidateArgumentSpecNoProvidedArguments(t *testing.T) {
	conn := newFakeConn(nil)
	res, err := moduleValidateArgumentSpec(context.Background(), conn, map[string]any{
		"argument_spec": map[string]any{
			"name": map[string]any{"type": "str", "required": true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed: nothing provided, required arg missing")
	}
}

func TestModuleValidateArgumentSpecMissingSpec(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleValidateArgumentSpec(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing argument_spec")
	}
}

func TestModuleValidateArgumentSpecWrongSpecType(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleValidateArgumentSpec(context.Background(), conn, map[string]any{"argument_spec": "not a map"}); err == nil {
		t.Fatal("want error for non-map argument_spec")
	}
	if _, err := moduleValidateArgumentSpec(context.Background(), conn, map[string]any{
		"argument_spec": map[string]any{}, "provided_arguments": "not a map",
	}); err == nil {
		t.Fatal("want error for non-map provided_arguments")
	}
}
