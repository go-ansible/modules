package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

const (
	javaCertTestExecutable   = "keytool"
	javaCertTestAlias        = "mycert"
	javaCertTestKeystorePath = "/etc/ssl/certs/java/cacerts"
	javaCertTestKeystorePass = "changeit"
)

func javaCertTestListCmd(alias string) string {
	return shellQuote(javaCertTestExecutable) + " -list -alias " + shellQuote(alias) +
		" -keystore " + shellQuote(javaCertTestKeystorePath) + " -storepass " + shellQuote(javaCertTestKeystorePass) + " >/dev/null 2>&1"
}

func TestModuleJavaCertBinaryMissing(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v keytool": {RC: 1},
	})
	res, err := moduleJavaCert(context.Background(), conn, map[string]any{
		"cert_alias": javaCertTestAlias, "keystore_path": javaCertTestKeystorePath,
		"keystore_pass": javaCertTestKeystorePass, "cert_path": "/opt/certs/rootca.crt",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed: keytool binary missing")
	}
}

func TestModuleJavaCertPresentAlready(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v keytool":                   {RC: 0},
		javaCertTestListCmd(javaCertTestAlias): {RC: 0},
	})
	res, err := moduleJavaCert(context.Background(), conn, map[string]any{
		"cert_alias": javaCertTestAlias, "keystore_path": javaCertTestKeystorePath,
		"keystore_pass": javaCertTestKeystorePass, "cert_path": "/opt/certs/rootca.crt",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged: alias already present")
	}
}

func TestModuleJavaCertImportFromCertPath(t *testing.T) {
	certPath := "/opt/certs/rootca.crt"
	importCmd := shellQuote(javaCertTestExecutable) + " -importcert -noprompt -alias " + shellQuote(javaCertTestAlias) +
		" -file " + shellQuote(certPath) +
		" -keystore " + shellQuote(javaCertTestKeystorePath) +
		" -storepass " + shellQuote(javaCertTestKeystorePass)
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v keytool":                              {RC: 0},
		javaCertTestListCmd(javaCertTestAlias):            {RC: 1},
		"test -e " + shellQuote(javaCertTestKeystorePath): {RC: 0},
		importCmd: {RC: 0},
	})
	res, err := moduleJavaCert(context.Background(), conn, map[string]any{
		"cert_alias": javaCertTestAlias, "keystore_path": javaCertTestKeystorePath,
		"keystore_pass": javaCertTestKeystorePass, "cert_path": certPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleJavaCertImportFromCertPathWithTrustAndType(t *testing.T) {
	certPath := "/opt/certs/rootca.crt"
	importCmd := shellQuote(javaCertTestExecutable) + " -importcert -noprompt -alias " + shellQuote(javaCertTestAlias) +
		" -file " + shellQuote(certPath) +
		" -keystore " + shellQuote(javaCertTestKeystorePath) +
		" -storepass " + shellQuote(javaCertTestKeystorePass) +
		" -storetype " + shellQuote("JKS") +
		" -trustcacerts"
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v keytool":                              {RC: 0},
		javaCertTestListCmd(javaCertTestAlias):            {RC: 1},
		"test -e " + shellQuote(javaCertTestKeystorePath): {RC: 0},
		importCmd: {RC: 0},
	})
	res, err := moduleJavaCert(context.Background(), conn, map[string]any{
		"cert_alias": javaCertTestAlias, "keystore_path": javaCertTestKeystorePath,
		"keystore_pass": javaCertTestKeystorePass, "cert_path": certPath,
		"keystore_type": "JKS", "trust_cacert": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleJavaCertImportFromCertContent(t *testing.T) {
	tmp := "/tmp/java_cert-" + javaCertTestAlias + ".pem"
	importCmd := shellQuote(javaCertTestExecutable) + " -importcert -noprompt -alias " + shellQuote(javaCertTestAlias) +
		" -file " + shellQuote(tmp) +
		" -keystore " + shellQuote(javaCertTestKeystorePath) +
		" -storepass " + shellQuote(javaCertTestKeystorePass)
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v keytool":                              {RC: 0},
		javaCertTestListCmd(javaCertTestAlias):            {RC: 1},
		"test -e " + shellQuote(javaCertTestKeystorePath): {RC: 0},
		"cat > " + shellQuote(tmp):                        {RC: 0},
		importCmd:                                         {RC: 0},
	})
	res, err := moduleJavaCert(context.Background(), conn, map[string]any{
		"cert_alias": javaCertTestAlias, "keystore_path": javaCertTestKeystorePath,
		"keystore_pass": javaCertTestKeystorePass,
		"cert_content":  "-----BEGIN CERTIFICATE-----\n...\n-----END CERTIFICATE-----\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	found := false
	for i, c := range conn.Commands {
		if c == "cat > "+shellQuote(tmp) {
			if conn.Stdins[i] != "-----BEGIN CERTIFICATE-----\n...\n-----END CERTIFICATE-----\n" {
				t.Fatalf("stdin = %q", conn.Stdins[i])
			}
			found = true
		}
	}
	if !found {
		t.Fatal("want a cat > tmp command with cert_content as stdin")
	}
}

func TestModuleJavaCertImportFromCertURL(t *testing.T) {
	tmp := "/tmp/java_cert-" + javaCertTestAlias + ".pem"
	fetchCmd := "openssl s_client -connect " + shellQuote("google.com:443") +
		" -servername " + shellQuote("google.com") + " </dev/null 2>/dev/null | " +
		"sed -ne '/-BEGIN CERTIFICATE-/,/-END CERTIFICATE-/p' > " + shellQuote(tmp)
	importCmd := shellQuote(javaCertTestExecutable) + " -importcert -noprompt -alias " + shellQuote(javaCertTestAlias) +
		" -file " + shellQuote(tmp) +
		" -keystore " + shellQuote(javaCertTestKeystorePath) +
		" -storepass " + shellQuote(javaCertTestKeystorePass)
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v keytool":                              {RC: 0},
		javaCertTestListCmd(javaCertTestAlias):            {RC: 1},
		"test -e " + shellQuote(javaCertTestKeystorePath): {RC: 0},
		"command -v openssl":                              {RC: 0},
		fetchCmd:                                          {RC: 0},
		importCmd:                                         {RC: 0},
	})
	res, err := moduleJavaCert(context.Background(), conn, map[string]any{
		"cert_alias": javaCertTestAlias, "keystore_path": javaCertTestKeystorePath,
		"keystore_pass": javaCertTestKeystorePass,
		"cert_url":      "google.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleJavaCertImportFromCertURLOpensslMissing(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v keytool":                              {RC: 0},
		javaCertTestListCmd(javaCertTestAlias):            {RC: 1},
		"test -e " + shellQuote(javaCertTestKeystorePath): {RC: 0},
		"command -v openssl":                              {RC: 1},
	})
	res, err := moduleJavaCert(context.Background(), conn, map[string]any{
		"cert_alias": javaCertTestAlias, "keystore_path": javaCertTestKeystorePath,
		"keystore_pass": javaCertTestKeystorePass,
		"cert_url":      "google.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed: openssl missing")
	}
}

func TestModuleJavaCertImportFromPKCS12(t *testing.T) {
	pkcs12Path := "/tmp/importkeystore.p12"
	importCmd := shellQuote(javaCertTestExecutable) + " -importkeystore -noprompt" +
		" -srckeystore " + shellQuote(pkcs12Path) +
		" -srcstoretype PKCS12" +
		" -srcstorepass " + shellQuote("somepass") +
		" -srcalias " + shellQuote("default") +
		" -destkeystore " + shellQuote(javaCertTestKeystorePath) +
		" -deststorepass " + shellQuote(javaCertTestKeystorePass) +
		" -destalias " + shellQuote(javaCertTestAlias)
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v keytool":                              {RC: 0},
		javaCertTestListCmd(javaCertTestAlias):            {RC: 1},
		"test -e " + shellQuote(javaCertTestKeystorePath): {RC: 0},
		importCmd: {RC: 0},
	})
	res, err := moduleJavaCert(context.Background(), conn, map[string]any{
		"cert_alias": javaCertTestAlias, "keystore_path": javaCertTestKeystorePath,
		"keystore_pass": javaCertTestKeystorePass,
		"pkcs12_path":   pkcs12Path, "pkcs12_alias": "default", "pkcs12_password": "somepass",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleJavaCertAbsentPresent(t *testing.T) {
	deleteCmd := shellQuote(javaCertTestExecutable) + " -delete -noprompt -alias " + shellQuote(javaCertTestAlias) +
		" -keystore " + shellQuote(javaCertTestKeystorePath) + " -storepass " + shellQuote(javaCertTestKeystorePass)
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v keytool":                   {RC: 0},
		javaCertTestListCmd(javaCertTestAlias): {RC: 0},
		deleteCmd:                              {RC: 0},
	})
	res, err := moduleJavaCert(context.Background(), conn, map[string]any{
		"cert_alias": javaCertTestAlias, "keystore_path": javaCertTestKeystorePath,
		"keystore_pass": javaCertTestKeystorePass, "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleJavaCertAbsentNotPresent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v keytool":                   {RC: 0},
		javaCertTestListCmd(javaCertTestAlias): {RC: 1},
	})
	res, err := moduleJavaCert(context.Background(), conn, map[string]any{
		"cert_alias": javaCertTestAlias, "keystore_path": javaCertTestKeystorePath,
		"keystore_pass": javaCertTestKeystorePass, "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleJavaCertKeystoreCreateFalseMissing(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v keytool":                              {RC: 0},
		javaCertTestListCmd(javaCertTestAlias):            {RC: 1},
		"test -e " + shellQuote(javaCertTestKeystorePath): {RC: 1},
	})
	res, err := moduleJavaCert(context.Background(), conn, map[string]any{
		"cert_alias": javaCertTestAlias, "keystore_path": javaCertTestKeystorePath,
		"keystore_pass": javaCertTestKeystorePass, "cert_path": "/opt/certs/rootca.crt",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed: keystore missing and keystore_create is false")
	}
}

func TestModuleJavaCertNoSource(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v keytool":                              {RC: 0},
		javaCertTestListCmd(javaCertTestAlias):            {RC: 1},
		"test -e " + shellQuote(javaCertTestKeystorePath): {RC: 0},
	})
	if _, err := moduleJavaCert(context.Background(), conn, map[string]any{
		"cert_alias": javaCertTestAlias, "keystore_path": javaCertTestKeystorePath,
		"keystore_pass": javaCertTestKeystorePass,
	}); err == nil {
		t.Fatal("want error: no certificate source given")
	}
}

func TestModuleJavaCertMultipleSources(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v keytool":                              {RC: 0},
		javaCertTestListCmd(javaCertTestAlias):            {RC: 1},
		"test -e " + shellQuote(javaCertTestKeystorePath): {RC: 0},
	})
	if _, err := moduleJavaCert(context.Background(), conn, map[string]any{
		"cert_alias": javaCertTestAlias, "keystore_path": javaCertTestKeystorePath,
		"keystore_pass": javaCertTestKeystorePass,
		"cert_path":     "/opt/certs/rootca.crt", "cert_url": "google.com",
	}); err == nil {
		t.Fatal("want error: multiple certificate sources given")
	}
}

func TestModuleJavaCertValidation(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleJavaCert(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing cert_alias")
	}
	if _, err := moduleJavaCert(context.Background(), conn, map[string]any{"cert_alias": "a"}); err == nil {
		t.Fatal("want error for missing keystore_path")
	}
	if _, err := moduleJavaCert(context.Background(), conn, map[string]any{"cert_alias": "a", "keystore_path": "/x"}); err == nil {
		t.Fatal("want error for missing keystore_pass")
	}
	if _, err := moduleJavaCert(context.Background(), conn, map[string]any{
		"cert_alias": "a", "keystore_path": "/x", "keystore_pass": "y", "state": "bogus",
	}); err == nil {
		t.Fatal("want error for invalid state")
	}
}
