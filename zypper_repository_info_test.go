package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

const zypperReposXML = `<?xml version='1.0'?>
<stream>
<repo-list>
<repo alias="repo-oss" name="Main Repository" type="rpm-md" priority="99" enabled="1" autorefresh="1" gpgcheck="1">
<url>http://download.opensuse.org/tumbleweed/repo/oss/</url>
</repo>
<repo alias="repo-non-oss" name="Non-OSS Repository" type="rpm-md" priority="99" enabled="0" autorefresh="0" gpgcheck="0">
<url>http://download.opensuse.org/tumbleweed/repo/non-oss/</url>
</repo>
</repo-list>
</stream>
`

func TestModuleZypperRepositoryInfo(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"zypper --quiet --non-interactive --xmlout repos": {RC: 0, Stdout: zypperReposXML},
	})
	res, err := moduleZypperRepositoryInfo(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("want unchanged, unfailed result, res = %+v", res)
	}
	list, ok := res.Extra["repodatalist"].([]map[string]any)
	if !ok {
		t.Fatalf("repodatalist = %#v, want []map[string]any", res.Extra["repodatalist"])
	}
	if len(list) != 2 {
		t.Fatalf("len(repodatalist) = %d, want 2", len(list))
	}
	if list[0]["alias"] != "repo-oss" || list[0]["url"] != "http://download.opensuse.org/tumbleweed/repo/oss/" {
		t.Fatalf("repodatalist[0] = %#v", list[0])
	}
	if list[1]["enabled"] != "0" || list[1]["gpgcheck"] != "0" {
		t.Fatalf("repodatalist[1] = %#v", list[1])
	}
}

func TestModuleZypperRepositoryInfoNoRepos(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"zypper --quiet --non-interactive --xmlout repos": {RC: 6},
	})
	res, err := moduleZypperRepositoryInfo(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	list, ok := res.Extra["repodatalist"].([]map[string]any)
	if !ok {
		t.Fatalf("repodatalist = %#v", res.Extra["repodatalist"])
	}
	if len(list) != 0 {
		t.Fatalf("want empty repodatalist, got %v", list)
	}
}
