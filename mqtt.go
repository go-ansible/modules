package modules

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"os"
	"strconv"
	"time"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleMqtt implements Ansible's `mqtt` (community.general) module:
// publishes one message on an MQTT topic.
//
// Architectural note: real mqtt's own EXAMPLES carry exactly one
// example, and it uses `delegate_to: localhost` — the same detective
// signal wakeonlan.go's own doc comment used to establish that
// wakeonlan sends its packet from wherever the Go code itself runs
// (i.e. the control node), rather than composing a shell command
// through `conn` against a target. It's the same shape here: real
// mqtt's own NOTES name a broker (Mosquitto) and the Paho Python MQTT
// client library — nothing about a target host at all, since an MQTT
// publish is a single short-lived TCP (or TLS) connection, always most
// naturally opened from wherever the Ansible run itself happens.
// Consistent with that, this port opens the MQTT connection directly
// from the Go process using a minimal from-scratch MQTT 3.1.1 client
// (CONNECT/PUBLISH/DISCONNECT, and the PUBACK/PUBREC-PUBREL-PUBCOMP
// handshakes for qos 1/2) built on the standard library's net/crypto-tls
// packages — this port has no MQTT client dependency to reuse, the same
// position wakeonlan.go was in for raw UDP. `conn` is accepted (for
// Func signature compatibility with every other module) but is
// intentionally never used, exactly as in wakeonlan.go.
//
// Args: topic (string, required); payload (string, required) — the
// literal string "None" sends an empty/NULL payload, matching real
// mqtt's own documented special case; server (string, default
// "localhost"); port (int, default 1883); qos ("0"|"1"|"2", default
// "0"); retain (bool, default false); client_id (string, default
// "<hostname>-<pid>", approximating real mqtt's own "hostname + pid"
// default — the exact separator is this port's own choice, since real
// paho's own default has no documented separator either);
// username/password (string, optional); ca_cert (alias ca_certs),
// client_cert (alias certfile), client_key (alias keyfile) (string
// paths, optional, read from wherever this Go process runs — i.e. the
// control node, matching the delegate_to:localhost usage above) — any
// of these selects a TLS connection instead of plain TCP; tls_version
// ("tlsv1.1"|"tlsv1.2", optional) — pins both TLS min and max version
// when given.
//
// Simplifications vs real mqtt (real paho-backed): no automatic
// "highest supported TLS version" negotiation beyond Go's own crypto/tls
// defaults when tls_version is unset; a broker CONNACK failure or a
// mid-publish socket error is reported as this port's own Go error, not
// mapped onto paho's own specific reason-code strings.
func moduleMqtt(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	topic, err := requireString(args, "topic")
	if err != nil {
		return Result{}, err
	}
	payloadArg, err := requireString(args, "payload")
	if err != nil {
		return Result{}, err
	}
	payload := []byte(payloadArg)
	if payloadArg == "None" {
		payload = nil
	}

	server := argString(args, "server", "localhost")
	port := argInt(args, "port", 1883)
	qosStr := argString(args, "qos", "0")
	qos, err := strconv.Atoi(qosStr)
	if err != nil || qos < 0 || qos > 2 {
		return Result{}, errArg("mqtt: qos must be 0, 1, or 2, got %q", qosStr)
	}
	retain := argBool(args, "retain", false)

	clientID := argString(args, "client_id", "")
	if clientID == "" {
		host, _ := os.Hostname()
		clientID = fmt.Sprintf("%s-%d", host, os.Getpid())
	}
	username := argString(args, "username", "")
	password := argString(args, "password", "")

	caCert := argString(args, "ca_cert", argString(args, "ca_certs", ""))
	clientCert := argString(args, "client_cert", argString(args, "certfile", ""))
	clientKey := argString(args, "client_key", argString(args, "keyfile", ""))
	tlsVersion := argString(args, "tls_version", "")

	nc, err := mqttDial(server, port, caCert, clientCert, clientKey, tlsVersion)
	if err != nil {
		return Result{}, fmt.Errorf("mqtt: connecting to %s:%d: %w", server, port, err)
	}
	defer nc.Close()

	if err := mqttConnect(nc, clientID, username, password); err != nil {
		return Result{}, fmt.Errorf("mqtt: CONNECT: %w", err)
	}
	if err := mqttPublish(nc, topic, payload, byte(qos), retain); err != nil {
		return Result{}, fmt.Errorf("mqtt: PUBLISH: %w", err)
	}
	_, _ = nc.Write([]byte{0xE0, 0x00}) // DISCONNECT

	return Changed("published to " + topic), nil
}

func mqttDial(server string, port int, caCert, clientCert, clientKey, tlsVersion string) (net.Conn, error) {
	addr := net.JoinHostPort(server, strconv.Itoa(port))
	if caCert == "" && clientCert == "" && clientKey == "" && tlsVersion == "" {
		return net.DialTimeout("tcp", addr, 10*time.Second)
	}

	cfg := &tls.Config{ServerName: server}
	if caCert != "" {
		pem, err := os.ReadFile(caCert)
		if err != nil {
			return nil, err
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("no certificates found in %s", caCert)
		}
		cfg.RootCAs = pool
	}
	if clientCert != "" && clientKey != "" {
		cert, err := tls.LoadX509KeyPair(clientCert, clientKey)
		if err != nil {
			return nil, err
		}
		cfg.Certificates = []tls.Certificate{cert}
	}
	switch tlsVersion {
	case "tlsv1.1":
		cfg.MinVersion = tls.VersionTLS11
		cfg.MaxVersion = tls.VersionTLS11
	case "tlsv1.2":
		cfg.MinVersion = tls.VersionTLS12
		cfg.MaxVersion = tls.VersionTLS12
	case "":
	default:
		return nil, fmt.Errorf("unsupported tls_version %q", tlsVersion)
	}

	dialer := &net.Dialer{Timeout: 10 * time.Second}
	return tls.DialWithDialer(dialer, "tcp", addr, cfg)
}

// mqttEncodeString writes an MQTT UTF-8 string: a 2-byte big-endian
// length prefix followed by the raw bytes.
func mqttEncodeString(s string) []byte {
	b := make([]byte, 2+len(s))
	b[0] = byte(len(s) >> 8)
	b[1] = byte(len(s))
	copy(b[2:], s)
	return b
}

// mqttEncodeRemainingLength encodes n using MQTT's own variable-length
// integer scheme (up to 4 bytes, 7 data bits per byte, high bit
// signals "more bytes follow").
func mqttEncodeRemainingLength(n int) []byte {
	var out []byte
	for {
		b := byte(n % 128)
		n /= 128
		if n > 0 {
			b |= 0x80
		}
		out = append(out, b)
		if n == 0 {
			break
		}
	}
	return out
}

// mqttConnect sends an MQTT 3.1.1 CONNECT packet and reads back
// CONNACK, failing unless its return code is 0 (accepted).
func mqttConnect(nc net.Conn, clientID, username, password string) error {
	var flags byte = 0x02 // clean session
	var payload []byte
	payload = append(payload, mqttEncodeString(clientID)...)
	if username != "" {
		flags |= 0x80
		payload = append(payload, mqttEncodeString(username)...)
	}
	if password != "" {
		flags |= 0x40
		payload = append(payload, mqttEncodeString(password)...)
	}

	var varHeader []byte
	varHeader = append(varHeader, mqttEncodeString("MQTT")...)
	varHeader = append(varHeader, 0x04) // protocol level 4 (3.1.1)
	varHeader = append(varHeader, flags)
	varHeader = append(varHeader, 0x00, 0x3C) // keep alive: 60s

	body := append(varHeader, payload...)
	packet := append([]byte{0x10}, mqttEncodeRemainingLength(len(body))...)
	packet = append(packet, body...)

	_ = nc.SetDeadline(time.Now().Add(10 * time.Second))
	if _, err := nc.Write(packet); err != nil {
		return err
	}

	ack := make([]byte, 4)
	if _, err := readFull(nc, ack); err != nil {
		return fmt.Errorf("reading CONNACK: %w", err)
	}
	if ack[0] != 0x20 {
		return fmt.Errorf("unexpected packet type 0x%x waiting for CONNACK", ack[0])
	}
	if ack[3] != 0 {
		return fmt.Errorf("broker refused connection, return code %d", ack[3])
	}
	return nil
}

// mqttPublish sends a PUBLISH packet for topic/payload at the given
// QoS, then completes the QoS 1 (PUBACK) or QoS 2 (PUBREC/PUBREL/
// PUBCOMP) acknowledgement handshake; QoS 0 needs no acknowledgement.
func mqttPublish(nc net.Conn, topic string, payload []byte, qos byte, retain bool) error {
	const packetID = 1

	var varHeader []byte
	varHeader = append(varHeader, mqttEncodeString(topic)...)
	if qos > 0 {
		varHeader = append(varHeader, byte(packetID>>8), byte(packetID))
	}

	body := append(varHeader, payload...)
	head := byte(0x30) | (qos << 1)
	if retain {
		head |= 0x01
	}
	packet := append([]byte{head}, mqttEncodeRemainingLength(len(body))...)
	packet = append(packet, body...)

	_ = nc.SetDeadline(time.Now().Add(10 * time.Second))
	if _, err := nc.Write(packet); err != nil {
		return err
	}

	switch qos {
	case 0:
		return nil
	case 1:
		ack := make([]byte, 4)
		if _, err := readFull(nc, ack); err != nil {
			return fmt.Errorf("reading PUBACK: %w", err)
		}
		if ack[0] != 0x40 {
			return fmt.Errorf("unexpected packet type 0x%x waiting for PUBACK", ack[0])
		}
		return nil
	default: // qos == 2
		rec := make([]byte, 4)
		if _, err := readFull(nc, rec); err != nil {
			return fmt.Errorf("reading PUBREC: %w", err)
		}
		if rec[0] != 0x50 {
			return fmt.Errorf("unexpected packet type 0x%x waiting for PUBREC", rec[0])
		}
		rel := []byte{0x62, 0x02, byte(packetID >> 8), byte(packetID)}
		if _, err := nc.Write(rel); err != nil {
			return err
		}
		comp := make([]byte, 4)
		if _, err := readFull(nc, comp); err != nil {
			return fmt.Errorf("reading PUBCOMP: %w", err)
		}
		if comp[0] != 0x70 {
			return fmt.Errorf("unexpected packet type 0x%x waiting for PUBCOMP", comp[0])
		}
		return nil
	}
}

// readFull reads exactly len(buf) bytes from nc.
func readFull(nc net.Conn, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := nc.Read(buf[total:])
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}
