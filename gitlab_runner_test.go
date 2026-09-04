package modules

import (
	"context"
	"strings"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleGitlabRunnerCreateInstanceLevel(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"glab api 'runners/all?per_page=100' -X GET --paginate": {RC: 0, Stdout: "[]"},
		"glab api user/runners -X POST --input -":               {RC: 0, Stdout: `{"id":55,"description":"runner1","token":"glrt-abcdef"}`},
	})
	args := map[string]any{"description": "runner1"}
	res, err := moduleGitlabRunner(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	detail := res.Extra["runner"].(gitlabRunnerDetail)
	if detail.Token != "glrt-abcdef" {
		t.Fatalf("runner = %#v", detail)
	}
}

func TestModuleGitlabRunnerAbsentDeletes(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"glab api 'runners/all?per_page=100' -X GET --paginate": {RC: 0, Stdout: `[{"id":55,"description":"runner1"}]`},
		"glab api runners/55 -X DELETE":                         {RC: 0},
	})
	args := map[string]any{"description": "runner1", "state": "absent"}
	res, err := moduleGitlabRunner(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleGitlabRunnerLegacyRegistrationToken(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"glab api 'runners/all?per_page=100' -X GET --paginate": {RC: 0, Stdout: "[]"},
		"glab api runners -X POST --input -":                    {RC: 0, Stdout: `{"id":56,"description":"runner2","token":"glrt-xyz"}`},
	})
	args := map[string]any{"description": "runner2", "registration_token": "reg-tok"}
	res, err := moduleGitlabRunner(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	// the registration_token must never appear on the command line
	for _, c := range conn.Commands {
		if strings.Contains(c, "reg-tok") {
			t.Fatalf("registration_token leaked into command line: %q", c)
		}
	}
}
