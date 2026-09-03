package modules

import (
	"context"
	"io"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

// javaKeystoreFakeConn is a scripted remoteexec.Connection like
// fakeConn (see fakeconn_test.go's own doc comment for why a scripted
// fake — not a real Local connection — is this module's own test
// pattern: it needs openssl/keytool, and regenerating a real keystore
// isn't something to do from a unit test), but STATEFUL: it hands each
// command's registered handler its own call count, so a single test
// can script "doesn't exist yet" for a path's FIRST `test -e` probe
// and "exists now" for a LATER one after this module's own create
// step — something fakeConn's static command->Result map can't express.
// Scoped to this file only; fakeconn_test.go itself is untouched.
type javaKeystoreFakeConn struct {
	handlers map[string]func(call int) remoteexec.Result
	calls    map[string]int
	Commands []string
	Stdins   []string
}

func newJavaKeystoreFakeConn(handlers map[string]func(call int) remoteexec.Result) *javaKeystoreFakeConn {
	return &javaKeystoreFakeConn{handlers: handlers, calls: map[string]int{}}
}

func (f *javaKeystoreFakeConn) Exec(ctx context.Context, cmd string, stdin io.Reader) (remoteexec.Result, error) {
	f.Commands = append(f.Commands, cmd)
	if stdin != nil {
		data, _ := io.ReadAll(stdin)
		f.Stdins = append(f.Stdins, string(data))
	} else {
		f.Stdins = append(f.Stdins, "")
	}
	f.calls[cmd]++
	if h, ok := f.handlers[cmd]; ok {
		return h(f.calls[cmd]), nil
	}
	return remoteexec.Result{}, nil
}

func (f *javaKeystoreFakeConn) Put(ctx context.Context, localPath, remotePath string, opts remoteexec.PutOptions) error {
	return nil
}
func (f *javaKeystoreFakeConn) Fetch(ctx context.Context, remotePath, localPath string) error {
	return nil
}
func (f *javaKeystoreFakeConn) Remove(ctx context.Context, remotePath string) error { return nil }
func (f *javaKeystoreFakeConn) TempPath(base string) string                         { return "/tmp/" + base }
func (f *javaKeystoreFakeConn) Close() error                                        { return nil }

var _ remoteexec.Connection = (*javaKeystoreFakeConn)(nil)

func always(res remoteexec.Result) func(int) remoteexec.Result {
	return func(int) remoteexec.Result { return res }
}

func TestModuleJavaKeystoreCreatesNew(t *testing.T) {
	conn := newJavaKeystoreFakeConn(map[string]func(int) remoteexec.Result{
		"command -v openssl && command -v keytool": always(remoteexec.Result{RC: 0}),
		"test -e /etc/security/keystore.jks": func(call int) remoteexec.Result {
			if call == 1 {
				return remoteexec.Result{RC: 1} // doesn't exist yet
			}
			return remoteexec.Result{RC: 0} // exists after keytool import
		},
		"openssl pkcs12 -export -name example -in /tmp/java_keystore-example-cert.pem -inkey /tmp/java_keystore-example-key.pem -out /tmp/java_keystore-example.p12 -passout stdin": always(remoteexec.Result{RC: 0}),
		"keytool -importkeystore -destkeystore /etc/security/keystore.jks -srckeystore /tmp/java_keystore-example.p12 -srcstoretype pkcs12 -alias example -noprompt":                always(remoteexec.Result{RC: 0}),
	})
	res, err := moduleJavaKeystore(context.Background(), conn, map[string]any{
		"name": "example", "dest": "/etc/security/keystore.jks", "password": "changeit",
		"certificate": "CERT-DATA", "private_key": "KEY-DATA",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || !res.Changed {
		t.Fatalf("res = %+v, commands = %v", res, conn.Commands)
	}
	// Confirm the password was piped over stdin, never as a command-line
	// argument (see moduleJavaKeystore's own doc comment).
	foundStdin := false
	for _, s := range conn.Stdins {
		if s == "changeit\nchangeit" {
			foundStdin = true
		}
	}
	if !foundStdin {
		t.Fatalf("stdins = %v, want the export password piped over stdin", conn.Stdins)
	}
}

func TestModuleJavaKeystorePkcs12TypeSkipsKeytool(t *testing.T) {
	conn := newJavaKeystoreFakeConn(map[string]func(int) remoteexec.Result{
		"command -v openssl && command -v keytool": always(remoteexec.Result{RC: 0}),
		"test -e /etc/security/keystore.p12":       always(remoteexec.Result{RC: 1}),
		"openssl pkcs12 -export -name example -in /tmp/java_keystore-example-cert.pem -inkey /tmp/java_keystore-example-key.pem -out /tmp/java_keystore-example.p12 -passout stdin": always(remoteexec.Result{RC: 0}),
		"mv /tmp/java_keystore-example.p12 /etc/security/keystore.p12": always(remoteexec.Result{RC: 0}),
	})
	res, err := moduleJavaKeystore(context.Background(), conn, map[string]any{
		"name": "example", "dest": "/etc/security/keystore.p12", "password": "changeit",
		"certificate": "CERT-DATA", "private_key": "KEY-DATA", "keystore_type": "pkcs12",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || !res.Changed {
		t.Fatalf("res = %+v, commands = %v", res, conn.Commands)
	}
	for _, c := range conn.Commands {
		if c == "mv /tmp/java_keystore-example.p12 /etc/security/keystore.p12" {
			return
		}
	}
	t.Fatalf("commands = %v, want a direct mv (no keytool import)", conn.Commands)
}

func TestModuleJavaKeystoreUnchangedWhenFingerprintMatches(t *testing.T) {
	conn := newJavaKeystoreFakeConn(map[string]func(int) remoteexec.Result{
		"command -v openssl && command -v keytool":                                         always(remoteexec.Result{RC: 0}),
		"test -e /etc/security/keystore.jks":                                               always(remoteexec.Result{RC: 0}),
		"openssl x509 -noout -in /tmp/java_keystore-example-cert.pem -fingerprint -sha256": always(remoteexec.Result{RC: 0, Stdout: "sha256 Fingerprint=AA:BB:CC\n"}),
		"keytool -list -alias example -keystore /etc/security/keystore.jks -v":             always(remoteexec.Result{RC: 0, Stdout: "Alias name: example\nSHA256: AA:BB:CC\n"}),
	})
	res, err := moduleJavaKeystore(context.Background(), conn, map[string]any{
		"name": "example", "dest": "/etc/security/keystore.jks", "password": "changeit",
		"certificate": "CERT-DATA", "private_key": "KEY-DATA",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || res.Changed {
		t.Fatalf("res = %+v, commands = %v", res, conn.Commands)
	}
}

func TestModuleJavaKeystoreRegeneratesWhenFingerprintDiffers(t *testing.T) {
	conn := newJavaKeystoreFakeConn(map[string]func(int) remoteexec.Result{
		"command -v openssl && command -v keytool":                                         always(remoteexec.Result{RC: 0}),
		"test -e /etc/security/keystore.jks":                                               always(remoteexec.Result{RC: 0}),
		"openssl x509 -noout -in /tmp/java_keystore-example-cert.pem -fingerprint -sha256": always(remoteexec.Result{RC: 0, Stdout: "sha256 Fingerprint=DD:EE:FF\n"}),
		"keytool -list -alias example -keystore /etc/security/keystore.jks -v":             always(remoteexec.Result{RC: 0, Stdout: "SHA256: AA:BB:CC\n"}),
		"rm -f /etc/security/keystore.jks":                                                 always(remoteexec.Result{RC: 0}),
		"openssl pkcs12 -export -name example -in /tmp/java_keystore-example-cert.pem -inkey /tmp/java_keystore-example-key.pem -out /tmp/java_keystore-example.p12 -passout stdin": always(remoteexec.Result{RC: 0}),
		"keytool -importkeystore -destkeystore /etc/security/keystore.jks -srckeystore /tmp/java_keystore-example.p12 -srcstoretype pkcs12 -alias example -noprompt":                always(remoteexec.Result{RC: 0}),
	})
	res, err := moduleJavaKeystore(context.Background(), conn, map[string]any{
		"name": "example", "dest": "/etc/security/keystore.jks", "password": "changeit",
		"certificate": "CERT-DATA", "private_key": "KEY-DATA",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || !res.Changed {
		t.Fatalf("res = %+v", res)
	}
	found := false
	for _, c := range conn.Commands {
		if c == "rm -f /etc/security/keystore.jks" {
			found = true
		}
	}
	if !found {
		t.Fatal("want the existing keystore removed before the keytool import")
	}
}

func TestModuleJavaKeystoreForceAlwaysRecreates(t *testing.T) {
	conn := newJavaKeystoreFakeConn(map[string]func(int) remoteexec.Result{
		"command -v openssl && command -v keytool": always(remoteexec.Result{RC: 0}),
		"test -e /etc/security/keystore.jks":       always(remoteexec.Result{RC: 0}),
		"rm -f /etc/security/keystore.jks":         always(remoteexec.Result{RC: 0}),
		"openssl pkcs12 -export -name example -in /tmp/java_keystore-example-cert.pem -inkey /tmp/java_keystore-example-key.pem -out /tmp/java_keystore-example.p12 -passout stdin": always(remoteexec.Result{RC: 0}),
		"keytool -importkeystore -destkeystore /etc/security/keystore.jks -srckeystore /tmp/java_keystore-example.p12 -srcstoretype pkcs12 -alias example -noprompt":                always(remoteexec.Result{RC: 0}),
	})
	res, err := moduleJavaKeystore(context.Background(), conn, map[string]any{
		"name": "example", "dest": "/etc/security/keystore.jks", "password": "changeit",
		"certificate": "CERT-DATA", "private_key": "KEY-DATA", "force": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || !res.Changed {
		t.Fatalf("res = %+v", res)
	}
	for _, c := range conn.Commands {
		if c == "keytool -list -alias example -keystore /etc/security/keystore.jks -v" {
			t.Fatal("keytool -list should not run when force is true")
		}
	}
}

func TestModuleJavaKeystoreCryptographyBackendUnsupported(t *testing.T) {
	conn := newJavaKeystoreFakeConn(nil)
	res, err := moduleJavaKeystore(context.Background(), conn, map[string]any{
		"name": "example", "dest": "/etc/security/keystore.jks", "password": "changeit",
		"certificate": "CERT-DATA", "private_key": "KEY-DATA", "ssl_backend": "cryptography",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed for ssl_backend=cryptography")
	}
}

func TestModuleJavaKeystoreRequiresExactlyOneCertSource(t *testing.T) {
	conn := newJavaKeystoreFakeConn(nil)
	if _, err := moduleJavaKeystore(context.Background(), conn, map[string]any{
		"name": "example", "dest": "/x", "password": "y",
		"certificate": "a", "certificate_path": "/b", "private_key": "k",
	}); err == nil {
		t.Fatal("want error for both certificate and certificate_path")
	}
}

func TestModuleJavaKeystoreMissingArgs(t *testing.T) {
	conn := newJavaKeystoreFakeConn(nil)
	if _, err := moduleJavaKeystore(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing name/dest/password")
	}
}
