package modules

import (
	"context"
	"testing"
)

func TestModuleCobblerSystemQueryByName(t *testing.T) {
	conn := &cobblerFakeConn{on: map[string][]string{
		"login": {xmlrpcStringResponse("TOKEN")},
		"find_system": {xmlrpcArrayResponse(
			`<value><struct><member><name>name</name><value><string>myhost</string></value></member></struct></value>`,
		)},
	}}
	res, err := moduleCobblerSystem(context.Background(), conn, map[string]any{
		"host": "cobbler01", "name": "myhost", "state": "query",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	sys, _ := res.Extra["system"].(map[string]any)
	if sys["name"] != "myhost" {
		t.Fatalf("system = %v", res.Extra["system"])
	}
	for _, want := range []string{"login", "find_system"} {
		found := false
		for _, c := range conn.Calls {
			if c == want {
				found = true
			}
		}
		if !found {
			t.Errorf("expected a call to %s, got %v", want, conn.Calls)
		}
	}
}

func TestModuleCobblerSystemQueryAll(t *testing.T) {
	conn := &cobblerFakeConn{on: map[string][]string{
		"login": {xmlrpcStringResponse("TOKEN")},
		"get_systems": {xmlrpcArrayResponse(
			`<value><struct><member><name>name</name><value><string>a</string></value></member></struct></value>`,
			`<value><struct><member><name>name</name><value><string>b</string></value></member></struct></value>`,
		)},
	}}
	res, err := moduleCobblerSystem(context.Background(), conn, map[string]any{
		"host": "cobbler01", "state": "query",
	})
	if err != nil {
		t.Fatal(err)
	}
	systems, _ := res.Extra["systems"].([]any)
	if len(systems) != 2 {
		t.Fatalf("systems = %v", res.Extra["systems"])
	}
}

func TestModuleCobblerSystemAbsentAlreadyGone(t *testing.T) {
	conn := &cobblerFakeConn{on: map[string][]string{
		"login":       {xmlrpcStringResponse("TOKEN")},
		"find_system": {xmlrpcArrayResponse()},
	}}
	res, err := moduleCobblerSystem(context.Background(), conn, map[string]any{
		"host": "cobbler01", "name": "myhost", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("res = %+v, want unchanged (already absent)", res)
	}
	for _, c := range conn.Calls {
		if c == "remove_system" {
			t.Fatalf("remove_system should not have been called: %v", conn.Calls)
		}
	}
}

func TestModuleCobblerSystemAbsentRemoves(t *testing.T) {
	conn := &cobblerFakeConn{on: map[string][]string{
		"login": {xmlrpcStringResponse("TOKEN")},
		"find_system": {
			xmlrpcArrayResponse(`<value><struct><member><name>name</name><value><string>myhost</string></value></member></struct></value>`),
			xmlrpcArrayResponse(),
		},
		"remove_system": {xmlrpcBoolResponse(true)},
	}}
	res, err := moduleCobblerSystem(context.Background(), conn, map[string]any{
		"host": "cobbler01", "name": "myhost", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v, want changed", res)
	}
	found := false
	for _, c := range conn.Calls {
		if c == "remove_system" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected remove_system call, got %v", conn.Calls)
	}
}

func TestModuleCobblerSystemCreateNew(t *testing.T) {
	conn := &cobblerFakeConn{on: map[string][]string{
		"login": {xmlrpcStringResponse("TOKEN")},
		"find_system": {
			xmlrpcArrayResponse(),
			xmlrpcArrayResponse(`<value><struct><member><name>name</name><value><string>myhost</string></value></member></struct></value>`),
		},
		"new_system":    {xmlrpcStringResponse("sys::1")},
		"modify_system": {xmlrpcBoolResponse(true)},
		"save_system":   {xmlrpcBoolResponse(true)},
	}}
	res, err := moduleCobblerSystem(context.Background(), conn, map[string]any{
		"host": "cobbler01",
		"name": "myhost",
		"properties": map[string]any{
			"profile": "CentOS6-x86_64",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v, want changed", res)
	}
	wantOrder := []string{"login", "find_system", "new_system", "modify_system", "modify_system", "save_system", "find_system"}
	if len(conn.Calls) != len(wantOrder) {
		t.Fatalf("calls = %v, want %v", conn.Calls, wantOrder)
	}
	for i, w := range wantOrder {
		if conn.Calls[i] != w {
			t.Fatalf("calls = %v, want %v", conn.Calls, wantOrder)
		}
	}
}

func TestModuleCobblerSystemExistingUnchanged(t *testing.T) {
	systemXML := `<value><struct>` +
		`<member><name>name</name><value><string>myhost</string></value></member>` +
		`<member><name>profile</name><value><string>CentOS6-x86_64</string></value></member>` +
		`</struct></value>`
	conn := &cobblerFakeConn{on: map[string][]string{
		"login":             {xmlrpcStringResponse("TOKEN")},
		"find_system":       {xmlrpcArrayResponse(systemXML)},
		"version":           {xmlrpcStringResponse("3.3.7")},
		"get_system_handle": {xmlrpcStringResponse("myhost")},
	}}
	res, err := moduleCobblerSystem(context.Background(), conn, map[string]any{
		"host": "cobbler01",
		"name": "myhost",
		"properties": map[string]any{
			"profile": "CentOS6-x86_64",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("res = %+v, want unchanged (property already matches)", res)
	}
	for _, c := range conn.Calls {
		if c == "modify_system" || c == "save_system" {
			t.Fatalf("did not expect a %s call for a no-op property update: %v", c, conn.Calls)
		}
	}
}

func TestModuleCobblerSystemUnknownProperty(t *testing.T) {
	systemXML := `<value><struct><member><name>name</name><value><string>myhost</string></value></member></struct></value>`
	conn := &cobblerFakeConn{on: map[string][]string{
		"login":             {xmlrpcStringResponse("TOKEN")},
		"find_system":       {xmlrpcArrayResponse(systemXML)},
		"version":           {xmlrpcStringResponse("3.3.7")},
		"get_system_handle": {xmlrpcStringResponse("myhost")},
		"modify_system":     {xmlrpcBoolResponse(true)},
		"save_system":       {xmlrpcBoolResponse(true)},
	}}
	res, err := moduleCobblerSystem(context.Background(), conn, map[string]any{
		"host": "cobbler01",
		"name": "myhost",
		"properties": map[string]any{
			"not_a_real_property": "x",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	warnings, _ := res.Extra["warnings"].([]string)
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v", res.Extra["warnings"])
	}
}

func TestModuleCobblerSystemBadState(t *testing.T) {
	conn := &cobblerFakeConn{}
	if _, err := moduleCobblerSystem(context.Background(), conn, map[string]any{
		"name": "x", "state": "bogus",
	}); err == nil {
		t.Fatal("want error for invalid state")
	}
}
