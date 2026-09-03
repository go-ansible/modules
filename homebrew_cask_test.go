package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleHomebrewCaskInstall(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"brew list --cask google-chrome >/dev/null 2>&1": {RC: 1},
		"brew install --cask google-chrome":              {RC: 0},
	})
	res, err := moduleHomebrewCask(context.Background(), conn, map[string]any{"name": "google-chrome"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}

func TestModuleHomebrewCaskAlreadyInstalled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"brew list --cask google-chrome >/dev/null 2>&1": {RC: 0},
	})
	res, err := moduleHomebrewCask(context.Background(), conn, map[string]any{"name": "google-chrome"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleHomebrewCaskAbsent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"brew list --cask google-chrome >/dev/null 2>&1": {RC: 0},
		"brew uninstall --cask google-chrome":            {RC: 0},
	})
	res, err := moduleHomebrewCask(context.Background(), conn, map[string]any{"name": "google-chrome", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleHomebrewCaskLatest(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"brew upgrade --cask google-chrome": {RC: 0},
	})
	res, err := moduleHomebrewCask(context.Background(), conn, map[string]any{"name": "google-chrome", "state": "latest"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleHomebrewCaskHeadRejected(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleHomebrewCask(context.Background(), conn, map[string]any{"name": "google-chrome", "state": "head"}); err == nil {
		t.Fatal("want error: head is not valid for casks")
	}
}

func TestModuleHomebrewCaskLinkedRejected(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleHomebrewCask(context.Background(), conn, map[string]any{"name": "google-chrome", "state": "linked"}); err == nil {
		t.Fatal("want error: linked is not valid for casks")
	}
}

func TestModuleHomebrewCaskUnlinkedRejected(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleHomebrewCask(context.Background(), conn, map[string]any{"name": "google-chrome", "state": "unlinked"}); err == nil {
		t.Fatal("want error: unlinked is not valid for casks")
	}
}

func TestModuleHomebrewCaskMissingName(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleHomebrewCask(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing name")
	}
}
