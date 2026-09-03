package modules

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestModuleWakeonlan(t *testing.T) {
	lc, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer lc.Close()
	_, portStr, _ := net.SplitHostPort(lc.LocalAddr().String())

	type recvResult struct {
		buf []byte
		n   int
	}
	recv := make(chan recvResult, 1)
	go func() {
		buf := make([]byte, 256)
		n, _, err := lc.ReadFrom(buf)
		if err != nil {
			return
		}
		recv <- recvResult{buf, n}
	}()

	conn := local()
	res, err := moduleWakeonlan(context.Background(), conn, map[string]any{
		"mac":       "00:11:22:33:44:55",
		"broadcast": "127.0.0.1",
		"port":      portStr,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}

	select {
	case r := <-recv:
		if r.n != 102 {
			t.Fatalf("packet length = %d, want 102", r.n)
		}
		for i := 0; i < 6; i++ {
			if r.buf[i] != 0xFF {
				t.Fatalf("byte %d = %x, want 0xFF", i, r.buf[i])
			}
		}
		want := []byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55}
		for rep := 0; rep < 16; rep++ {
			got := r.buf[6+rep*6 : 6+rep*6+6]
			for i := range want {
				if got[i] != want[i] {
					t.Fatalf("repeat %d mac byte %d = %x, want %x", rep, i, got[i], want[i])
				}
			}
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for magic packet")
	}
}

func TestModuleWakeonlanInvalidMAC(t *testing.T) {
	conn := local()
	if _, err := moduleWakeonlan(context.Background(), conn, map[string]any{"mac": "not-a-mac"}); err == nil {
		t.Fatal("want error for invalid mac")
	}
}

func TestModuleWakeonlanMissingArg(t *testing.T) {
	conn := local()
	if _, err := moduleWakeonlan(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing mac")
	}
}

func TestWakeonlanMagicPacketHyphenated(t *testing.T) {
	p, err := wakeonlanMagicPacket("00-11-22-33-44-55")
	if err != nil {
		t.Fatal(err)
	}
	if len(p) != 102 {
		t.Fatalf("len = %d", len(p))
	}
}
