package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleMssqlDbCreate(t *testing.T) {
	existsQuery := "SET NOCOUNT ON; SELECT name FROM master.sys.databases WHERE name = " + mssqlQuoteLiteral("mydb")
	conn := newFakeConn(map[string]remoteexec.Result{
		"sqlcmd -S dbhost -b -h -1 -W -Q " + shellQuote(existsQuery):              {RC: 0, Stdout: ""},
		"sqlcmd -S dbhost -b -h -1 -W -Q " + shellQuote("CREATE DATABASE [mydb]"): {RC: 0},
	})
	res, err := moduleMssqlDb(context.Background(), conn, map[string]any{"name": "mydb", "login_host": "dbhost"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleMssqlDbAlreadyPresent(t *testing.T) {
	existsQuery := "SET NOCOUNT ON; SELECT name FROM master.sys.databases WHERE name = " + mssqlQuoteLiteral("mydb")
	conn := newFakeConn(map[string]remoteexec.Result{
		"sqlcmd -S dbhost -b -h -1 -W -Q " + shellQuote(existsQuery): {RC: 0, Stdout: "mydb"},
	})
	res, err := moduleMssqlDb(context.Background(), conn, map[string]any{"name": "mydb", "login_host": "dbhost"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleMssqlDbAbsent(t *testing.T) {
	existsQuery := "SET NOCOUNT ON; SELECT name FROM master.sys.databases WHERE name = " + mssqlQuoteLiteral("mydb")
	conn := newFakeConn(map[string]remoteexec.Result{
		"sqlcmd -S dbhost -b -h -1 -W -Q " + shellQuote(existsQuery):                                                     {RC: 0, Stdout: "mydb"},
		"sqlcmd -S dbhost -b -h -1 -W -Q " + shellQuote("ALTER DATABASE [mydb] SET single_user WITH ROLLBACK IMMEDIATE"): {RC: 0},
		"sqlcmd -S dbhost -b -h -1 -W -Q " + shellQuote("DROP DATABASE [mydb]"):                                          {RC: 0},
	})
	res, err := moduleMssqlDb(context.Background(), conn, map[string]any{
		"name": "mydb", "login_host": "dbhost", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleMssqlDbAbsentAlreadyGone(t *testing.T) {
	existsQuery := "SET NOCOUNT ON; SELECT name FROM master.sys.databases WHERE name = " + mssqlQuoteLiteral("mydb")
	conn := newFakeConn(map[string]remoteexec.Result{
		"sqlcmd -S dbhost -b -h -1 -W -Q " + shellQuote(existsQuery): {RC: 0, Stdout: ""},
	})
	res, err := moduleMssqlDb(context.Background(), conn, map[string]any{
		"name": "mydb", "login_host": "dbhost", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleMssqlDbNamedInstanceRejectsPort(t *testing.T) {
	conn := newFakeConn(nil)
	_, err := moduleMssqlDb(context.Background(), conn, map[string]any{
		"name": "mydb", "login_host": `dbhost\inst1`, "login_port": "1433",
	})
	if err == nil {
		t.Fatal("want error for login_port with a named instance")
	}
}

func TestModuleMssqlDbMissingName(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleMssqlDb(context.Background(), conn, map[string]any{"login_host": "dbhost"}); err == nil {
		t.Fatal("want error for missing name")
	}
}

func TestModuleMssqlDbImportMissingTarget(t *testing.T) {
	existsQuery := "SET NOCOUNT ON; SELECT name FROM master.sys.databases WHERE name = " + mssqlQuoteLiteral("mydb")
	conn := newFakeConn(map[string]remoteexec.Result{
		"sqlcmd -S dbhost -b -h -1 -W -Q " + shellQuote(existsQuery): {RC: 0, Stdout: "mydb"},
		"test -e " + shellQuote("/tmp/nonexistent-dump.sql"):         {RC: 1},
	})
	res, err := moduleMssqlDb(context.Background(), conn, map[string]any{
		"name": "mydb", "login_host": "dbhost", "state": "import", "target": "/tmp/nonexistent-dump.sql",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed for missing target file")
	}
}
