package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleHwcSmnTopicCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v hcloud":                      {RC: 0},
		"hcloud SMN ListTopics --name=my-topic":  {RC: 0, Stdout: `{"topics":[]}`},
		"hcloud SMN CreateTopic --name=my-topic": {RC: 0, Stdout: `{"topic_urn":"urn:smn:region:project:my-topic","name":"my-topic"}`},
	})
	res, err := moduleHwcSmnTopic(context.Background(), conn, map[string]any{"name": "my-topic"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["id"] != "urn:smn:region:project:my-topic" {
		t.Fatalf("id = %v", res.Extra["id"])
	}
}

func TestModuleHwcSmnTopicAbsentDeletes(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v hcloud":                            {RC: 0},
		"hcloud SMN ListTopics --name=my-topic":        {RC: 0, Stdout: `{"topics":[{"name":"my-topic","topic_urn":"urn:smn:x"}]}`},
		"hcloud SMN DeleteTopic --topic_urn=urn:smn:x": {RC: 0},
	})
	args := map[string]any{"name": "my-topic", "state": "absent"}
	res, err := moduleHwcSmnTopic(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}
