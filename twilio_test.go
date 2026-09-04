package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleTwilioSendSingle(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v twilio": {RC: 0},
		"TWILIO_ACCOUNT_SID=ACXXX TWILIO_AUTH_TOKEN=tok twilio api:core:messages:create --from +15552014545 --to +15553035681 --body 'All servers with webserver role are now configured.'": {
			RC: 0,
		},
	})
	res, err := moduleTwilio(context.Background(), conn, map[string]any{
		"msg":         "All servers with webserver role are now configured.",
		"account_sid": "ACXXX",
		"auth_token":  "tok",
		"from_number": "+15552014545",
		"to_number":   "+15553035681",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if !res.Changed {
		t.Fatal("want Changed=true")
	}
}

func TestModuleTwilioSendMultipleRecipients(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v twilio": {RC: 0},
		"TWILIO_ACCOUNT_SID=ACXXX TWILIO_AUTH_TOKEN=tok twilio api:core:messages:create --from +15553258899 --to +15551113232 --body 'This server configuration is now complete.'": {
			RC: 0,
		},
		"TWILIO_ACCOUNT_SID=ACXXX TWILIO_AUTH_TOKEN=tok twilio api:core:messages:create --from +15553258899 --to +12025551235 --body 'This server configuration is now complete.'": {
			RC: 0,
		},
	})
	res, err := moduleTwilio(context.Background(), conn, map[string]any{
		"msg":         "This server configuration is now complete.",
		"account_sid": "ACXXX",
		"auth_token":  "tok",
		"from_number": "+15553258899",
		"to_numbers":  []any{"+15551113232", "+12025551235"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || !res.Changed {
		t.Fatalf("res = %+v", res)
	}
	sent, ok := res.Extra["sent_to"].([]string)
	if !ok || len(sent) != 2 {
		t.Fatalf("sent_to = %+v", res.Extra["sent_to"])
	}
}

func TestModuleTwilioMMS(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v twilio": {RC: 0},
		"TWILIO_ACCOUNT_SID=ACXXX TWILIO_AUTH_TOKEN=tok twilio api:core:messages:create --from +15552014545 --to +15553035681 --body 'Deployment complete!' --media-url https://demo.twilio.com/logo.png": {
			RC: 0,
		},
	})
	res, err := moduleTwilio(context.Background(), conn, map[string]any{
		"msg":         "Deployment complete!",
		"account_sid": "ACXXX",
		"auth_token":  "tok",
		"from_number": "+15552014545",
		"to_number":   "+15553035681",
		"media_url":   "https://demo.twilio.com/logo.png",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleTwilioSendFailure(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v twilio": {RC: 0},
		"TWILIO_ACCOUNT_SID=ACXXX TWILIO_AUTH_TOKEN=tok twilio api:core:messages:create --from +15552014545 --to +15553035681 --body hi": {
			RC: 1, Stderr: "invalid number",
		},
	})
	res, err := moduleTwilio(context.Background(), conn, map[string]any{
		"msg":         "hi",
		"account_sid": "ACXXX",
		"auth_token":  "tok",
		"from_number": "+15552014545",
		"to_number":   "+15553035681",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want Failed, res = %+v", res)
	}
}

func TestModuleTwilioMissingArgs(t *testing.T) {
	conn := newFakeConn(nil)
	_, err := moduleTwilio(context.Background(), conn, map[string]any{})
	if err == nil {
		t.Fatal("want error for missing required arguments")
	}
}
