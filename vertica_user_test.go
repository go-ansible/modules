package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func verticaUserFactsQuery(user string) string {
	return "select u.user_name, u.is_locked, p.acctexpired, u.profile_name, u.resource_pool, u.all_roles, u.default_roles " +
		"from users u join password_auditor p on p.user_id = u.user_id " +
		"where not u.is_super_user and u.user_name ilike " + verticaQuoteLiteral(user)
}

func TestModuleVerticaUserCreate(t *testing.T) {
	factsQuery := verticaUserFactsQuery("myuser")
	conn := newFakeConn(map[string]remoteexec.Result{
		"vsql -h localhost -p 5433 -U dbadmin -X -A -t -c " + shellQuote(factsQuery):                                  {RC: 0, Stdout: ""},
		"vsql -h localhost -p 5433 -U dbadmin -X -A -t -c " + shellQuote("create user myuser identified by 'secret'"): {RC: 0},
	})
	res, err := moduleVerticaUser(context.Background(), conn, map[string]any{
		"user": "myuser", "password": "secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleVerticaUserAbsentAlreadyGone(t *testing.T) {
	factsQuery := verticaUserFactsQuery("myuser")
	conn := newFakeConn(map[string]remoteexec.Result{
		"vsql -h localhost -p 5433 -U dbadmin -X -A -t -c " + shellQuote(factsQuery): {RC: 0, Stdout: ""},
	})
	res, err := moduleVerticaUser(context.Background(), conn, map[string]any{"user": "myuser", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleVerticaUserAbsentDrops(t *testing.T) {
	factsQuery := verticaUserFactsQuery("myuser")
	conn := newFakeConn(map[string]remoteexec.Result{
		"vsql -h localhost -p 5433 -U dbadmin -X -A -t -c " + shellQuote(factsQuery):         {RC: 0, Stdout: "myuser|f|f|||"},
		"vsql -h localhost -p 5433 -U dbadmin -X -A -t -c " + shellQuote("drop user myuser"): {RC: 0},
	})
	res, err := moduleVerticaUser(context.Background(), conn, map[string]any{"user": "myuser", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleVerticaUserLockedState(t *testing.T) {
	factsQuery := verticaUserFactsQuery("myuser")
	conn := newFakeConn(map[string]remoteexec.Result{
		"vsql -h localhost -p 5433 -U dbadmin -X -A -t -c " + shellQuote(factsQuery):                       {RC: 0, Stdout: "myuser|f|f|||"},
		"vsql -h localhost -p 5433 -U dbadmin -X -A -t -c " + shellQuote("alter user myuser account lock"): {RC: 0},
	})
	res, err := moduleVerticaUser(context.Background(), conn, map[string]any{"user": "myuser", "state": "locked"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleVerticaUserMissingUser(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleVerticaUser(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing user")
	}
}

func TestModuleVerticaUserUnchanged(t *testing.T) {
	factsQuery := verticaUserFactsQuery("myuser")
	conn := newFakeConn(map[string]remoteexec.Result{
		"vsql -h localhost -p 5433 -U dbadmin -X -A -t -c " + shellQuote(factsQuery): {RC: 0, Stdout: "myuser|f|f|||"},
	})
	res, err := moduleVerticaUser(context.Background(), conn, map[string]any{"user": "myuser"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}
