package modules

import (
	"context"
	"net"
	"testing"
	"time"

	remoteexec "github.com/go-remoteexec/transport"
)

// mqttFakeBroker accepts exactly one TCP connection, reads a CONNECT
// packet (replying CONNACK success), then reads one PUBLISH packet and
// reports its topic/payload/qos/retain to the caller via ch.
type mqttPublished struct {
	topic   string
	payload []byte
	qos     byte
	retain  bool
}

func mqttFakeBroker(t *testing.T) (addr string, ch chan mqttPublished) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ch = make(chan mqttPublished, 1)
	go func() {
		defer ln.Close()
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		_ = c.SetDeadline(time.Now().Add(5 * time.Second))

		// Read CONNECT fixed header.
		head := make([]byte, 1)
		if _, err := readFull(c, head); err != nil {
			return
		}
		remLen, err := mqttReadRemainingLength(c)
		if err != nil {
			return
		}
		body := make([]byte, remLen)
		if _, err := readFull(c, body); err != nil {
			return
		}
		// CONNACK: session-present=0, return code=0.
		if _, err := c.Write([]byte{0x20, 0x02, 0x00, 0x00}); err != nil {
			return
		}

		// Read PUBLISH fixed header.
		if _, err := readFull(c, head); err != nil {
			return
		}
		pubType := head[0]
		qos := (pubType >> 1) & 0x03
		retain := pubType&0x01 != 0
		remLen, err = mqttReadRemainingLength(c)
		if err != nil {
			return
		}
		body = make([]byte, remLen)
		if _, err := readFull(c, body); err != nil {
			return
		}
		topicLen := int(body[0])<<8 | int(body[1])
		topic := string(body[2 : 2+topicLen])
		payloadStart := 2 + topicLen
		if qos > 0 {
			payloadStart += 2
		}
		payload := append([]byte(nil), body[payloadStart:]...)

		ch <- mqttPublished{topic: topic, payload: payload, qos: qos, retain: retain}

		if qos == 1 {
			_, _ = c.Write([]byte{0x40, 0x02, body[payloadStart-2], body[payloadStart-1]})
		} else if qos == 2 {
			_, _ = c.Write([]byte{0x50, 0x02, body[payloadStart-2], body[payloadStart-1]})
			pubrel := make([]byte, 4)
			_, _ = readFull(c, pubrel)
			_, _ = c.Write([]byte{0x70, 0x02, pubrel[2], pubrel[3]})
		}
	}()
	return ln.Addr().String(), ch
}

// mqttReadRemainingLength mirrors the decode side of
// mqttEncodeRemainingLength, for the test broker's own use.
func mqttReadRemainingLength(c net.Conn) (int, error) {
	multiplier := 1
	value := 0
	b := make([]byte, 1)
	for {
		if _, err := readFull(c, b); err != nil {
			return 0, err
		}
		value += int(b[0]&0x7F) * multiplier
		if b[0]&0x80 == 0 {
			break
		}
		multiplier *= 128
	}
	return value, nil
}

func TestModuleMqttPublishQoS0(t *testing.T) {
	addr, ch := mqttFakeBroker(t)
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	port := 0
	for _, c := range portStr {
		port = port*10 + int(c-'0')
	}

	res, err := moduleMqtt(context.Background(), nil, map[string]any{
		"topic":   "service/ansible/test",
		"payload": "hello world",
		"server":  host,
		"port":    port,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}

	select {
	case pub := <-ch:
		if pub.topic != "service/ansible/test" {
			t.Fatalf("topic = %q", pub.topic)
		}
		if string(pub.payload) != "hello world" {
			t.Fatalf("payload = %q", pub.payload)
		}
		if pub.qos != 0 || pub.retain {
			t.Fatalf("qos=%d retain=%v", pub.qos, pub.retain)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for broker to observe PUBLISH")
	}
}

func TestModuleMqttNonePayload(t *testing.T) {
	addr, ch := mqttFakeBroker(t)
	host, portStr, _ := net.SplitHostPort(addr)
	port := 0
	for _, c := range portStr {
		port = port*10 + int(c-'0')
	}
	_, err := moduleMqtt(context.Background(), nil, map[string]any{
		"topic": "t", "payload": "None", "server": host, "port": port,
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case pub := <-ch:
		if len(pub.payload) != 0 {
			t.Fatalf("payload = %q, want empty for \"None\"", pub.payload)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out")
	}
}

func TestModuleMqttMissingTopic(t *testing.T) {
	_, err := moduleMqtt(context.Background(), nil, map[string]any{"payload": "x"})
	if err == nil {
		t.Fatal("want error for missing topic")
	}
}

func TestModuleMqttBadQos(t *testing.T) {
	_, err := moduleMqtt(context.Background(), nil, map[string]any{
		"topic": "t", "payload": "x", "qos": "9",
	})
	if err == nil {
		t.Fatal("want error for invalid qos")
	}
}

func TestModuleMqttConnUnused(t *testing.T) {
	// conn is intentionally never dereferenced by moduleMqtt (like
	// wakeonlan.go); passing a nil remoteexec.Connection must not panic
	// before we even reach the network code (a bad arg should fail
	// first).
	var conn remoteexec.Connection
	_, err := moduleMqtt(context.Background(), conn, map[string]any{})
	if err == nil {
		t.Fatal("want error for missing topic/payload")
	}
}
