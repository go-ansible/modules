package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

const spectrumBinaryCheckCmd = `test -x "$SPECROOT/vnmsh/create" -a -x "$SPECROOT/vnmsh/seek"`
const spectrumSeekCmd = `cd "$SPECROOT/vnmsh" && ./seek attr=0x12d7f val=10.10.5.1 lh=0x100000`

func spectrumBaseArgs() map[string]any {
	return map[string]any{
		"device":       "10.10.5.1",
		"community":    "secret",
		"landscape":    "0x100000",
		"url":          "http://oneclick.example.com:8080",
		"url_username": "username",
		"url_password": "password",
	}
}

func TestModuleSpectrumDeviceCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		spectrumBinaryCheckCmd: {RC: 0},
		spectrumSeekCmd:        {RC: 1},
		`cd "$SPECROOT/vnmsh" && ./create model ip=10.10.5.1 comm=secret lh=0x100000 agentport=161`: {RC: 0, Stdout: "model created mh=0x1007ab"},
	})
	res, err := moduleSpectrumDevice(context.Background(), conn, spectrumBaseArgs())
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("want changed, not failed; res = %+v", res)
	}
	dev, _ := res.Extra["device"].(map[string]any)
	if dev["model_handle"] != "0x1007ab" {
		t.Fatalf("unexpected device: %+v", dev)
	}
}

func TestModuleSpectrumDeviceCreateAlreadyExists(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		spectrumBinaryCheckCmd: {RC: 0},
		spectrumSeekCmd:        {RC: 0, Stdout: "mh=0x1007ab"},
	})
	res, err := moduleSpectrumDevice(context.Background(), conn, spectrumBaseArgs())
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("want unchanged, not failed; res = %+v", res)
	}
}

func TestModuleSpectrumDeviceAbsentRemoves(t *testing.T) {
	args := spectrumBaseArgs()
	args["state"] = "absent"
	delete(args, "community")
	conn := newFakeConn(map[string]remoteexec.Result{
		spectrumBinaryCheckCmd: {RC: 0},
		spectrumSeekCmd:        {RC: 0, Stdout: "mh=0x1007ab"},
		`cd "$SPECROOT/vnmsh" && ./destroy model mh=0x1007ab -n`: {RC: 0},
	})
	res, err := moduleSpectrumDevice(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("want changed, not failed; res = %+v", res)
	}
}

func TestModuleSpectrumDeviceAbsentAlreadyGone(t *testing.T) {
	args := spectrumBaseArgs()
	args["state"] = "absent"
	delete(args, "community")
	conn := newFakeConn(map[string]remoteexec.Result{
		spectrumBinaryCheckCmd: {RC: 0},
		spectrumSeekCmd:        {RC: 1},
	})
	res, err := moduleSpectrumDevice(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("want unchanged, not failed; res = %+v", res)
	}
}

func TestModuleSpectrumDeviceMissingBinary(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		spectrumBinaryCheckCmd: {RC: 1},
	})
	res, err := moduleSpectrumDevice(context.Background(), conn, spectrumBaseArgs())
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want failed, res = %+v", res)
	}
}

func TestModuleSpectrumDeviceMissingArgs(t *testing.T) {
	conn := newFakeConn(nil)
	_, err := moduleSpectrumDevice(context.Background(), conn, map[string]any{"device": "1.2.3.4"})
	if err == nil {
		t.Fatal("want error for missing required args")
	}
}
