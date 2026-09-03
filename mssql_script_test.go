package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleMssqlScriptRuns(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"sqlcmd -S dbhost -h -1 -W": {RC: 0, Stdout: "1\n"},
	})
	res, err := moduleMssqlScript(context.Background(), conn, map[string]any{
		"login_host": "dbhost", "script": "SELECT 1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["stdout"] != "1\n" {
		t.Fatalf("stdout = %v", res.Extra["stdout"])
	}
	if len(conn.Stdins) != 1 || conn.Stdins[0] != "SELECT 1" {
		t.Fatalf("stdins = %v, want script sent via stdin", conn.Stdins)
	}
}

func TestModuleMssqlScriptFailure(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"sqlcmd -S dbhost -h -1 -W": {RC: 1, Stderr: "Invalid object name 'bogus'."},
	})
	res, err := moduleMssqlScript(context.Background(), conn, map[string]any{
		"login_host": "dbhost", "script": "SELECT * FROM bogus",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed for a query error")
	}
}

func TestModuleMssqlScriptParamsSubstitution(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"sqlcmd -S dbhost -h -1 -W": {RC: 0, Stdout: "msdb\n"},
	})
	_, err := moduleMssqlScript(context.Background(), conn, map[string]any{
		"login_host": "dbhost",
		"script":     "SELECT name FROM sys.databases WHERE name = %(dbname)s",
		"params":     map[string]any{"dbname": "msdb"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "SELECT name FROM sys.databases WHERE name = 'msdb'"
	if len(conn.Stdins) != 1 || conn.Stdins[0] != want {
		t.Fatalf("stdins = %v, want %q", conn.Stdins, want)
	}
}

func TestModuleMssqlScriptTransactionWraps(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"sqlcmd -S dbhost -h -1 -W -b": {RC: 0, Stdout: ""},
	})
	_, err := moduleMssqlScript(context.Background(), conn, map[string]any{
		"login_host": "dbhost", "script": "UPDATE t SET x = 1", "transaction": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	got := conn.Stdins[0]
	if got != "BEGIN TRANSACTION;\nUPDATE t SET x = 1\nIF @@TRANCOUNT > 0 COMMIT TRANSACTION;\n" {
		t.Fatalf("stdin = %q", got)
	}
}

func TestModuleMssqlScriptBatchCount(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"sqlcmd -S dbhost -h -1 -W": {RC: 0, Stdout: ""},
	})
	res, err := moduleMssqlScript(context.Background(), conn, map[string]any{
		"login_host": "dbhost",
		"script":     "SELECT 1\nGO\nSELECT 2\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Extra["batches"] != 2 {
		t.Fatalf("batches = %v", res.Extra["batches"])
	}
}

func TestModuleMssqlScriptMissingScript(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleMssqlScript(context.Background(), conn, map[string]any{"login_host": "dbhost"}); err == nil {
		t.Fatal("want error for missing script")
	}
}
