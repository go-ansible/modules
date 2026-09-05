package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleMattermostPosts(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v mmctl": {RC: 0},
		"mmctl post create team:town --message hello": {RC: 0},
	})
	res, err := moduleMattermost(context.Background(), conn, map[string]any{
		"channel": "team:town", "text": "hello",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}

func TestModuleMattermostMissingText(t *testing.T) {
	conn := newFakeConn(nil)
	_, err := moduleMattermost(context.Background(), conn, map[string]any{"channel": "team:town"})
	if err == nil {
		t.Fatal("want error for missing text")
	}
}

func TestModuleMattermostChannelWithoutTeamFails(t *testing.T) {
	conn := newFakeConn(nil)
	_, err := moduleMattermost(context.Background(), conn, map[string]any{"channel": "town", "text": "hello"})
	if err == nil {
		t.Fatal("want error for channel without team prefix")
	}
}

func TestModuleMattermostAttachmentsUnsupported(t *testing.T) {
	conn := newFakeConn(nil)
	res, err := moduleMattermost(context.Background(), conn, map[string]any{
		"channel": "team:town", "attachments": []any{map[string]any{"text": "x"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want failed, res = %+v", res)
	}
}

func TestModuleMattermostMissingBinary(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v mmctl": {RC: 1},
	})
	res, err := moduleMattermost(context.Background(), conn, map[string]any{
		"channel": "team:town", "text": "hello",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want failed, res = %+v", res)
	}
}

func TestModuleMattermostNonZeroExit(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v mmctl": {RC: 0},
		"mmctl post create team:town --message hello": {RC: 1, Stderr: "not authenticated"},
	})
	res, err := moduleMattermost(context.Background(), conn, map[string]any{
		"channel": "team:town", "text": "hello",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want failed, res = %+v", res)
	}
}
