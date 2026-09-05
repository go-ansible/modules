package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func jiraBaseArgs(op string) map[string]any {
	return map[string]any{"uri": "https://example.atlassian.net", "username": "u", "password": "p", "operation": op}
}

func TestModuleJiraMissingBinary(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v acli": {RC: 1},
	})
	args := jiraBaseArgs("fetch")
	args["issue"] = "HSP-1"
	res, err := moduleJira(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want failed, res = %+v", res)
	}
}

func TestModuleJiraCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v acli": {RC: 0},
		"acli jira workitem create -p ANS -s 'Example Issue' -t Task -d 'Created using Ansible' --json": {RC: 0, Stdout: `{"key":"ANS-1"}`},
	})
	args := jiraBaseArgs("create")
	args["project"] = "ANS"
	args["summary"] = "Example Issue"
	args["issuetype"] = "Task"
	args["description"] = "Created using Ansible"
	res, err := moduleJira(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	meta, _ := res.Extra["meta"].(map[string]any)
	if meta["key"] != "ANS-1" {
		t.Fatalf("meta = %+v", res.Extra["meta"])
	}
}

func TestModuleJiraCreateUnsupportedFieldFails(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v acli": {RC: 0},
	})
	args := jiraBaseArgs("create")
	args["project"] = "ANS"
	args["summary"] = "Example Issue"
	args["issuetype"] = "Task"
	args["fields"] = map[string]any{"customfield_13225": "test"}
	if _, err := moduleJira(context.Background(), conn, args); err == nil {
		t.Fatal("want error: acli create has no equivalent for arbitrary custom fields")
	}
}

func TestModuleJiraComment(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v acli": {RC: 0},
		"acli jira workitem comment create -k ANS-1 -b 'A comment added by Ansible' --json": {RC: 0, Stdout: `{"id":"10001"}`},
	})
	args := jiraBaseArgs("comment")
	args["issue"] = "ANS-1"
	args["comment"] = "A comment added by Ansible"
	res, err := moduleJira(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleJiraCommentVisibilityFails(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v acli": {RC: 0},
	})
	args := jiraBaseArgs("comment")
	args["issue"] = "ANS-1"
	args["comment"] = "text"
	args["comment_visibility"] = map[string]any{"type": "role", "value": "Developers"}
	res, err := moduleJira(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want failed (no atomic acli equivalent for comment_visibility), res = %+v", res)
	}
}

func TestModuleJiraEdit(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v acli": {RC: 0},
		"acli jira workitem edit -k ANS-1 -y -l autocreated,ansible --json": {RC: 0, Stdout: `{}`},
	})
	args := jiraBaseArgs("edit")
	args["issue"] = "ANS-1"
	args["fields"] = map[string]any{"labels": []any{"autocreated", "ansible"}}
	res, err := moduleJira(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleJiraEditAssignee(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v acli": {RC: 0},
		"acli jira workitem edit -k ANS-1 -y -a ssmith --json": {RC: 0, Stdout: `{}`},
	})
	args := jiraBaseArgs("edit")
	args["issue"] = "ANS-1"
	args["assignee"] = "ssmith"
	res, err := moduleJira(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleJiraUpdateAlwaysFails(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v acli": {RC: 0},
	})
	args := jiraBaseArgs("update")
	args["issue"] = "ANS-1"
	args["fields"] = map[string]any{"labels": []any{map[string]any{"add": "x"}}}
	res, err := moduleJira(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want failed: operation=update has no acli equivalent, res = %+v", res)
	}
}

func TestModuleJiraFetch(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v acli": {RC: 0},
		"acli jira workitem view ANS-63 -f '*all' --json": {RC: 0, Stdout: `{"key":"ANS-63"}`},
	})
	args := jiraBaseArgs("fetch")
	args["issue"] = "ANS-63"
	res, err := moduleJira(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleJiraLink(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v acli": {RC: 0},
		"acli jira workitem link create --out MKY-1 --in HSP-1 --type Relates --yes --json": {RC: 0, Stdout: `{}`},
	})
	args := jiraBaseArgs("link")
	args["linktype"] = "Relates"
	args["inwardissue"] = "HSP-1"
	args["outwardissue"] = "MKY-1"
	res, err := moduleJira(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleJiraSearch(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v acli": {RC: 0},
		"acli jira workitem search --jql project=cmdb --limit 10 --json": {RC: 0, Stdout: `{"issues":[]}`},
	})
	args := jiraBaseArgs("search")
	args["jql"] = "project=cmdb"
	args["maxresults"] = 10
	res, err := moduleJira(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleJiraTransition(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v acli": {RC: 0},
		"acli jira workitem transition -k ANS-1 -s 'Resolve Issue' -y --json": {RC: 0, Stdout: `{}`},
	})
	args := jiraBaseArgs("transition")
	args["issue"] = "ANS-1"
	args["status"] = "Resolve Issue"
	res, err := moduleJira(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleJiraTransitionWithComment(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v acli": {RC: 0},
		"acli jira workitem transition -k ANS-1 -s Done -y --json":        {RC: 0, Stdout: `{}`},
		"acli jira workitem comment create -k ANS-1 -b 'All done' --json": {RC: 0, Stdout: `{}`},
	})
	args := jiraBaseArgs("transition")
	args["issue"] = "ANS-1"
	args["status"] = "Done"
	args["comment"] = "All done"
	res, err := moduleJira(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleJiraTransitionStatusIDFails(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v acli": {RC: 0},
	})
	args := jiraBaseArgs("transition")
	args["issue"] = "ANS-1"
	args["status_id"] = "5"
	res, err := moduleJira(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want failed: status_id has no verified acli equivalent, res = %+v", res)
	}
}

func TestModuleJiraAttachAlwaysFails(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v acli": {RC: 0},
	})
	args := jiraBaseArgs("attach")
	args["issue"] = "HSP-1"
	args["attachment"] = map[string]any{"filename": "report.xlsx"}
	res, err := moduleJira(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want failed: acli has no attachment upload verb, res = %+v", res)
	}
}

func TestModuleJiraWorklogAlwaysFails(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v acli": {RC: 0},
	})
	args := jiraBaseArgs("worklog")
	args["issue"] = "ANS-1"
	args["comment"] = "A worklog added by Ansible"
	res, err := moduleJira(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want failed: acli has no worklog command, res = %+v", res)
	}
}
