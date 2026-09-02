package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleAptRepositoryPPAAdd(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"add-apt-repository -y ppa:foo/bar": {RC: 0},
	})
	res, err := moduleAptRepository(context.Background(), conn, map[string]any{"repo": "ppa:foo/bar"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleAptRepositoryPPARemove(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"add-apt-repository -y --remove ppa:foo/bar": {RC: 0},
	})
	res, err := moduleAptRepository(context.Background(), conn, map[string]any{
		"repo": "ppa:foo/bar", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleAptRepositoryLineNew(t *testing.T) {
	repo := "deb http://example.com/ stable main"
	path := aptRepoFilename(repo)
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e " + shellQuote(path): {RC: 1},
		"mkdir -p /etc/apt/sources.list.d && printf '%s\\n' " + shellQuote(repo) + " > " + shellQuote(path): {RC: 0},
	})
	res, err := moduleAptRepository(context.Background(), conn, map[string]any{"repo": repo})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleAptRepositoryLineAlreadyPresent(t *testing.T) {
	repo := "deb http://example.com/ stable main"
	path := aptRepoFilename(repo)
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e " + shellQuote(path): {RC: 0},
		"cat " + shellQuote(path):     {RC: 0, Stdout: repo},
	})
	res, err := moduleAptRepository(context.Background(), conn, map[string]any{"repo": repo})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleAptRepositoryLineChangedContent(t *testing.T) {
	repo := "deb http://example.com/ stable main"
	path := aptRepoFilename(repo)
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e " + shellQuote(path): {RC: 0},
		"cat " + shellQuote(path):     {RC: 0, Stdout: "deb http://old.example.com/ stable main"},
		"mkdir -p /etc/apt/sources.list.d && printf '%s\\n' " + shellQuote(repo) + " > " + shellQuote(path): {RC: 0},
	})
	res, err := moduleAptRepository(context.Background(), conn, map[string]any{"repo": repo})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleAptRepositoryLineAbsent(t *testing.T) {
	repo := "deb http://example.com/ stable main"
	path := aptRepoFilename(repo)
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e " + shellQuote(path): {RC: 0},
		"rm -f " + shellQuote(path):   {RC: 0},
	})
	res, err := moduleAptRepository(context.Background(), conn, map[string]any{"repo": repo, "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleAptRepositoryLineAbsentNotPresent(t *testing.T) {
	repo := "deb http://example.com/ stable main"
	path := aptRepoFilename(repo)
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e " + shellQuote(path): {RC: 1},
	})
	res, err := moduleAptRepository(context.Background(), conn, map[string]any{"repo": repo, "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleAptRepositoryUpdateCache(t *testing.T) {
	repo := "deb http://example.com/ stable main"
	path := aptRepoFilename(repo)
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e " + shellQuote(path): {RC: 1},
		"mkdir -p /etc/apt/sources.list.d && printf '%s\\n' " + shellQuote(repo) + " > " + shellQuote(path): {RC: 0},
		"DEBIAN_FRONTEND=noninteractive apt-get update -q":                                                  {RC: 0},
	})
	res, err := moduleAptRepository(context.Background(), conn, map[string]any{"repo": repo, "update_cache": true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	found := false
	for _, c := range conn.Commands {
		if c == "DEBIAN_FRONTEND=noninteractive apt-get update -q" {
			found = true
		}
	}
	if !found {
		t.Fatal("want apt-get update to have run")
	}
}

func TestModuleAptRepositoryMissingRepo(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleAptRepository(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error")
	}
}

func TestModuleAptRepositoryInvalidState(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleAptRepository(context.Background(), conn, map[string]any{
		"repo": "deb http://x/ stable main", "state": "bogus",
	}); err == nil {
		t.Fatal("want error")
	}
}

func TestAptRepoFilename(t *testing.T) {
	cases := map[string]string{
		"deb http://example.com/ stable main": "/etc/apt/sources.list.d/deb-http-example-com-stable-main.list",
		"":                                    "/etc/apt/sources.list.d/repository.list",
	}
	for in, want := range cases {
		if got := aptRepoFilename(in); got != want {
			t.Errorf("aptRepoFilename(%q) = %q, want %q", in, got, want)
		}
	}
	// A very long repo string is truncated.
	long := ""
	for i := 0; i < 100; i++ {
		long += "a "
	}
	if got := aptRepoFilename(long); len(got) > len("/etc/apt/sources.list.d/")+60+len(".list") {
		t.Errorf("aptRepoFilename(long) = %q, too long", got)
	}
}
