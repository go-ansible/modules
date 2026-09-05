package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleSendgridSingleRecipient(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v twilio": {RC: 0},
		"SENDGRID_API_KEY=SG.xxx twilio email:send --from ansible@mycompany.com --subject 'Deployment success.' --text 'The most recent Ansible deployment was successful.' --to ops@mycompany.com": {RC: 0},
	})
	res, err := moduleSendgrid(context.Background(), conn, map[string]any{
		"api_key":      "SG.xxx",
		"from_address": "ansible@mycompany.com",
		"to_addresses": []any{"ops@mycompany.com"},
		"subject":      "Deployment success.",
		"body":         "The most recent Ansible deployment was successful.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("want changed, not failed; res = %+v", res)
	}
}

func TestModuleSendgridMultipleRecipientsAndFromName(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v twilio": {RC: 0},
		"SENDGRID_API_KEY=SG.xxx twilio email:send --from 'Ops Team <build@mycompany.com>' --subject 'Build failure!.' --text 'Unable to pull source repository from Git server.' --to ops@mycompany.com --to devteam@mycompany.com": {RC: 0},
	})
	res, err := moduleSendgrid(context.Background(), conn, map[string]any{
		"api_key":      "SG.xxx",
		"from_address": "build@mycompany.com",
		"from_name":    "Ops Team",
		"to_addresses": []any{"ops@mycompany.com", "devteam@mycompany.com"},
		"subject":      "Build failure!.",
		"body":         "Unable to pull source repository from Git server.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}

func TestModuleSendgridUsernamePasswordUnsupported(t *testing.T) {
	conn := newFakeConn(nil)
	res, err := moduleSendgrid(context.Background(), conn, map[string]any{
		"username":     "u",
		"password":     "p",
		"from_address": "a@b.com",
		"to_addresses": []any{"c@d.com"},
		"subject":      "s",
		"body":         "b",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want failed (no twilio-cli equivalent), res = %+v", res)
	}
}

func TestModuleSendgridHTMLBodyUnsupported(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v twilio": {RC: 0},
	})
	res, err := moduleSendgrid(context.Background(), conn, map[string]any{
		"api_key":      "SG.xxx",
		"from_address": "a@b.com",
		"to_addresses": []any{"c@d.com"},
		"subject":      "s",
		"body":         "<b>hi</b>",
		"html_body":    true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want failed (no --html flag documented), res = %+v", res)
	}
}

func TestModuleSendgridAttachmentsUnsupported(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v twilio": {RC: 0},
	})
	res, err := moduleSendgrid(context.Background(), conn, map[string]any{
		"api_key":      "SG.xxx",
		"from_address": "a@b.com",
		"to_addresses": []any{"c@d.com"},
		"subject":      "s",
		"body":         "b",
		"attachments":  []any{"/tmp/file.txt"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want failed (no attachments equivalent documented), res = %+v", res)
	}
}

func TestModuleSendgridFailure(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v twilio": {RC: 0},
		"SENDGRID_API_KEY=SG.xxx twilio email:send --from a@b.com --subject s --text b --to c@d.com": {RC: 1, Stderr: "unauthorized"},
	})
	res, err := moduleSendgrid(context.Background(), conn, map[string]any{
		"api_key":      "SG.xxx",
		"from_address": "a@b.com",
		"to_addresses": []any{"c@d.com"},
		"subject":      "s",
		"body":         "b",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want failed, res = %+v", res)
	}
}

func TestModuleSendgridMissingArgs(t *testing.T) {
	conn := newFakeConn(nil)
	_, err := moduleSendgrid(context.Background(), conn, map[string]any{"api_key": "x"})
	if err == nil {
		t.Fatal("want error for missing required args")
	}
}
