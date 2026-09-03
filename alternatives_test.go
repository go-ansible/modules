package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleAlternativesInstallAndSelect(t *testing.T) {
	prefix := alternativesToolPrefix()
	conn := newFakeConn(map[string]remoteexec.Result{
		prefix + "$T --display java 2>/dev/null | grep -qF /usr/lib/jvm/java-7-openjdk-amd64/jre/bin/java": {RC: 1},
		prefix + "$T --install /usr/bin/java java /usr/lib/jvm/java-7-openjdk-amd64/jre/bin/java 50":       {RC: 0},
		prefix + "$T --query java 2>/dev/null | grep '^Value: '":                                           {RC: 1},
		prefix + "$T --set java /usr/lib/jvm/java-7-openjdk-amd64/jre/bin/java":                            {RC: 0},
	})
	res, err := moduleAlternatives(context.Background(), conn, map[string]any{
		"name": "java", "link": "/usr/bin/java", "path": "/usr/lib/jvm/java-7-openjdk-amd64/jre/bin/java",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleAlternativesAlreadySelected(t *testing.T) {
	prefix := alternativesToolPrefix()
	conn := newFakeConn(map[string]remoteexec.Result{
		prefix + "$T --display java 2>/dev/null | grep -qF /usr/bin/java3": {RC: 0},
		prefix + "$T --query java 2>/dev/null | grep '^Value: '":           {RC: 0, Stdout: "Value: /usr/bin/java3\n"},
	})
	res, err := moduleAlternatives(context.Background(), conn, map[string]any{
		"name": "java", "path": "/usr/bin/java3",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged: already installed and selected")
	}
}

func TestModuleAlternativesPresentOnly(t *testing.T) {
	prefix := alternativesToolPrefix()
	conn := newFakeConn(map[string]remoteexec.Result{
		prefix + "$T --display python 2>/dev/null | grep -qF /usr/bin/python3.5": {RC: 1},
		prefix + "$T --install /usr/bin/python python /usr/bin/python3.5 50":     {RC: 0},
	})
	res, err := moduleAlternatives(context.Background(), conn, map[string]any{
		"name": "python", "link": "/usr/bin/python", "path": "/usr/bin/python3.5", "state": "present",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed: newly installed")
	}
	for _, c := range conn.Commands {
		if c == prefix+"$T --set python /usr/bin/python3.5" {
			t.Fatal("state=present must not select the alternative")
		}
	}
}

func TestModuleAlternativesAuto(t *testing.T) {
	prefix := alternativesToolPrefix()
	conn := newFakeConn(map[string]remoteexec.Result{
		prefix + "$T --display python 2>/dev/null | grep -qF /usr/bin/python3.5": {RC: 0},
		prefix + "$T --query python 2>/dev/null | grep '^Status: '":              {RC: 0, Stdout: "Status: manual\n"},
		prefix + "$T --auto python":                                              {RC: 0},
	})
	res, err := moduleAlternatives(context.Background(), conn, map[string]any{
		"name": "python", "path": "/usr/bin/python3.5", "state": "auto",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed: switched to auto")
	}
}

func TestModuleAlternativesAbsent(t *testing.T) {
	prefix := alternativesToolPrefix()
	conn := newFakeConn(map[string]remoteexec.Result{
		prefix + "$T --display java 2>/dev/null >/dev/null": {RC: 0},
		prefix + "$T --remove java /usr/bin/java7":          {RC: 0},
	})
	res, err := moduleAlternatives(context.Background(), conn, map[string]any{
		"name": "java", "path": "/usr/bin/java7", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleAlternativesAbsentAlreadyGone(t *testing.T) {
	prefix := alternativesToolPrefix()
	conn := newFakeConn(map[string]remoteexec.Result{
		prefix + "$T --display java 2>/dev/null >/dev/null": {RC: 1},
	})
	res, err := moduleAlternatives(context.Background(), conn, map[string]any{
		"name": "java", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged: already absent")
	}
}

func TestModuleAlternativesMissingArgs(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleAlternatives(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing name")
	}
}

func TestModuleAlternativesSubcommands(t *testing.T) {
	got := alternativesInstallCmd("java", "/usr/bin/java", "/opt/java/bin/java", 50, "", []map[string]any{
		{"name": "keytool", "link": "/usr/bin/keytool", "path": "/opt/java/bin/keytool"},
	})
	want := alternativesToolPrefix() + "$T --install /usr/bin/java java /opt/java/bin/java 50 --slave /usr/bin/keytool keytool /opt/java/bin/keytool"
	if got != want {
		t.Errorf("alternativesInstallCmd = %q, want %q", got, want)
	}
}
