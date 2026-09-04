package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleOnepasswordInfoSimplePassword(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"op get account": {RC: 0},
		"op get item 'My 1Password item'": {
			RC: 0, Stdout: `{"overview":{"title":"My 1Password item"},"details":{"password":"hunter2"}}`,
		},
	})
	res, err := moduleOnepasswordInfo(context.Background(), conn, map[string]any{
		"search_terms": []any{"My 1Password item"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	op, ok := res.Extra["onepassword"].(map[string]any)
	if !ok {
		t.Fatalf("onepassword = %+v", res.Extra["onepassword"])
	}
	item, ok := op["My 1Password item"].(map[string]any)
	if !ok || item["password"] != "hunter2" {
		t.Fatalf("item = %+v", item)
	}
}

func TestModuleOnepasswordInfoCustomFieldAndSection(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"op get account": {RC: 0},
		"op get item 'My item'": {
			RC: 0, Stdout: `{"overview":{"title":"My item"},"details":{"sections":[{"title":"Custom section name","fields":[{"t":"Custom field name","v":"the value"}]}]}}`,
		},
	})
	res, err := moduleOnepasswordInfo(context.Background(), conn, map[string]any{
		"search_terms": []any{
			map[string]any{"name": "My item", "field": "Custom field name", "section": "Custom section name"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	op := res.Extra["onepassword"].(map[string]any)
	item := op["My item"].(map[string]any)
	if item["Custom field name"] != "the value" {
		t.Fatalf("item = %+v", item)
	}
}

func TestModuleOnepasswordInfoDocument(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"op get account": {RC: 0},
		"op get item 'A doc'": {
			RC: 0, Stdout: `{"overview":{"title":"A doc"},"details":{"documentAttributes":{"fileName":"x.txt"}}}`,
		},
		"op get document 'A doc'": {RC: 0, Stdout: "the contents\n"},
	})
	res, err := moduleOnepasswordInfo(context.Background(), conn, map[string]any{
		"search_terms": []any{"A doc"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	op := res.Extra["onepassword"].(map[string]any)
	item := op["A doc"].(map[string]any)
	if item["document"] != "the contents" {
		t.Fatalf("item = %+v", item)
	}
}

func TestModuleOnepasswordInfoItemNotFound(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"op get account":             {RC: 0},
		"op get item 'Missing item'": {RC: 1, Stderr: "item not found"},
	})
	res, err := moduleOnepasswordInfo(context.Background(), conn, map[string]any{
		"search_terms": []any{"Missing item"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want Failed, res = %+v", res)
	}
}

func TestModuleOnepasswordInfoFieldNotFound(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"op get account": {RC: 0},
		"op get item 'My item'": {
			RC: 0, Stdout: `{"overview":{"title":"My item"},"details":{}}`,
		},
	})
	res, err := moduleOnepasswordInfo(context.Background(), conn, map[string]any{
		"search_terms": []any{
			map[string]any{"name": "My item", "field": "nonexistent"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want Failed, res = %+v", res)
	}
}

func TestModuleOnepasswordInfoNotSignedInNoAutoLogin(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"op get account": {RC: 1, Stderr: "you are not currently signed in"},
	})
	res, err := moduleOnepasswordInfo(context.Background(), conn, map[string]any{
		"search_terms": []any{"My item"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want Failed, res = %+v", res)
	}
}

func TestModuleOnepasswordInfoAutoLoginMasterPasswordOverStdin(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"op get account":         {RC: 1, Stderr: "not signed in"},
		"op signin --output=raw": {RC: 0, Stdout: "sess-token\n"},
		"op get item 'My item' --session=sess-token": {
			RC: 0, Stdout: `{"overview":{"title":"My item"},"details":{"password":"p"}}`,
		},
	})
	res, err := moduleOnepasswordInfo(context.Background(), conn, map[string]any{
		"search_terms": []any{"My item"},
		"auto_login":   map[string]any{"master_password": "mp"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if conn.Stdins[1] != "mp" {
		t.Fatalf("expected master_password piped to stdin, got %q", conn.Stdins[1])
	}
}

func TestModuleOnepasswordInfoMissingSearchTerms(t *testing.T) {
	conn := newFakeConn(nil)
	_, err := moduleOnepasswordInfo(context.Background(), conn, map[string]any{})
	if err == nil {
		t.Fatal("want error for missing search_terms")
	}
}
