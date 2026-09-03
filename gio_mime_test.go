package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleGioMimeAlreadySet(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v gio":                  {RC: 0},
		"gio --version":                   {RC: 0, Stdout: "2.80.0\n"},
		"gio mime x-scheme-handler/https": {RC: 0, Stdout: "Default application for 'x-scheme-handler/https': google-chrome.desktop\n"},
	})
	res, err := moduleGioMime(context.Background(), conn, map[string]any{
		"mime_type": "x-scheme-handler/https", "handler": "google-chrome.desktop",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["handler"] != "google-chrome.desktop" {
		t.Fatalf("handler = %v", res.Extra["handler"])
	}
	if res.Extra["version"] != "2.80.0" {
		t.Fatalf("version = %v", res.Extra["version"])
	}
}

func TestModuleGioMimeNoneSetYet(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v gio":                  {RC: 0},
		"gio --version":                   {RC: 0, Stdout: "2.80.0"},
		"gio mime x-scheme-handler/https": {RC: 1, Stderr: "No default applications for 'x-scheme-handler/https'\n"},
		"gio mime x-scheme-handler/https google-chrome.desktop": {RC: 0},
	})
	res, err := moduleGioMime(context.Background(), conn, map[string]any{
		"mime_type": "x-scheme-handler/https", "handler": "google-chrome.desktop",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v, commands = %v", res, conn.Commands)
	}
}

func TestModuleGioMimeChanges(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v gio":                  {RC: 0},
		"gio --version":                   {RC: 0, Stdout: "2.80.0"},
		"gio mime x-scheme-handler/https": {RC: 0, Stdout: "Default application for 'x-scheme-handler/https': firefox.desktop\n"},
		"gio mime x-scheme-handler/https google-chrome.desktop": {RC: 0},
	})
	res, err := moduleGioMime(context.Background(), conn, map[string]any{
		"mime_type": "x-scheme-handler/https", "handler": "google-chrome.desktop",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleGioMimeNotInstalled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v gio": {RC: 1},
	})
	res, err := moduleGioMime(context.Background(), conn, map[string]any{
		"mime_type": "x-scheme-handler/https", "handler": "google-chrome.desktop",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed when gio is not on the target")
	}
}

func TestModuleGioMimeMissingArgs(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleGioMime(context.Background(), conn, map[string]any{"handler": "x.desktop"}); err == nil {
		t.Fatal("want error for missing mime_type")
	}
	if _, err := moduleGioMime(context.Background(), conn, map[string]any{"mime_type": "text/plain"}); err == nil {
		t.Fatal("want error for missing handler")
	}
}
