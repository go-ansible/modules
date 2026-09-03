package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleXdgMimeAlreadySet(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v xdg-mime":                           {RC: 0},
		"xdg-mime --version":                            {RC: 0, Stdout: "xdg-mime 1.2.1\n"},
		"xdg-mime query default x-scheme-handler/https": {RC: 0, Stdout: "google-chrome.desktop\n"},
	})
	res, err := moduleXdgMime(context.Background(), conn, map[string]any{
		"mime_types": []any{"x-scheme-handler/https"}, "handler": "google-chrome.desktop",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["version"] != "1.2.1" {
		t.Fatalf("version = %v", res.Extra["version"])
	}
}

func TestModuleXdgMimeSetsMultiple(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v xdg-mime":                           {RC: 0},
		"xdg-mime --version":                            {RC: 0, Stdout: "xdg-mime 1.2.1"},
		"xdg-mime query default x-scheme-handler/http":  {RC: 0, Stdout: ""},
		"xdg-mime query default x-scheme-handler/https": {RC: 0, Stdout: "firefox.desktop\n"},
		"xdg-mime default google-chrome.desktop x-scheme-handler/http x-scheme-handler/https": {RC: 0},
	})
	res, err := moduleXdgMime(context.Background(), conn, map[string]any{
		"mime_types": []any{"x-scheme-handler/http", "x-scheme-handler/https"}, "handler": "google-chrome.desktop",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v, commands = %v", res, conn.Commands)
	}
	handlers := res.Extra["current_handlers"].([]string)
	if handlers[0] != "" || handlers[1] != "firefox.desktop" {
		t.Fatalf("current_handlers = %v", handlers)
	}
}

func TestModuleXdgMimeHandlerMustBeDesktopFile(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v xdg-mime": {RC: 0},
	})
	res, err := moduleXdgMime(context.Background(), conn, map[string]any{
		"mime_types": []any{"text/plain"}, "handler": "not-a-desktop-file",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed when handler doesn't end in .desktop")
	}
}

func TestModuleXdgMimeNotInstalled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v xdg-mime": {RC: 1},
	})
	res, err := moduleXdgMime(context.Background(), conn, map[string]any{
		"mime_types": []any{"text/plain"}, "handler": "foo.desktop",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed when xdg-mime is not on the target")
	}
}

func TestModuleXdgMimeMissingArgs(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleXdgMime(context.Background(), conn, map[string]any{"handler": "x.desktop"}); err == nil {
		t.Fatal("want error for missing mime_types")
	}
	if _, err := moduleXdgMime(context.Background(), conn, map[string]any{"mime_types": []any{"text/plain"}}); err == nil {
		t.Fatal("want error for missing handler")
	}
}
