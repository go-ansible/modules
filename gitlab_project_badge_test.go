package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleGitlabProjectBadgeCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"glab api 'projects/g%2Fp/badges?per_page=100' -X GET --paginate": {RC: 0, Stdout: "[]"},
		"glab api projects/g%2Fp/badges -X POST --input -":                {RC: 0, Stdout: `{"id":1,"link_url":"http://link","image_url":"http://img"}`},
	})
	args := map[string]any{"project": "g/p", "link_url": "http://link", "image_url": "http://img"}
	res, err := moduleGitlabProjectBadge(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	badge := res.Extra["badge"].(gitlabBadgeObj)
	if badge.ImageURL != "http://img" {
		t.Fatalf("badge = %#v", badge)
	}
}

func TestModuleGitlabProjectBadgeAbsentDeletes(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"glab api 'projects/g%2Fp/badges?per_page=100' -X GET --paginate": {RC: 0, Stdout: `[{"id":1,"link_url":"http://link","image_url":"http://img"}]`},
		"glab api projects/g%2Fp/badges/1 -X DELETE":                      {RC: 0},
	})
	args := map[string]any{"project": "g/p", "link_url": "http://link", "image_url": "http://img", "state": "absent"}
	res, err := moduleGitlabProjectBadge(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}
