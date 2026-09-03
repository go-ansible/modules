package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleKopiaRepositoryCreateFilesystem(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"kopia repository status --config-file=/etc/kopia/root.config":                                                       {RC: 1, Stdout: ""},
		"kopia repository create filesystem --path=/mnt/backup/kopia --password=secret --config-file=/etc/kopia/root.config": {RC: 0, Stdout: ""},
	})
	res, err := moduleKopiaRepository(context.Background(), conn, map[string]any{
		"state":    "created",
		"password": "secret",
		"config":   "/etc/kopia/root.config",
		"backend": map[string]any{
			"provider": "filesystem",
			"path":     "/mnt/backup/kopia",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v, commands = %v", res, conn.Commands)
	}
	if res.Changed {
		t.Fatalf("both status probes returned empty output, want unchanged: res = %+v", res)
	}
}

func TestModuleKopiaRepositoryAlreadyExistsIsIgnoredNotFailed(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"kopia repository status": {RC: 0, Stdout: "Connected to repository: filesystem:/mnt/backup/kopia\n"},
		"kopia repository create filesystem --path=/mnt/backup/kopia --password=secret": {
			RC: 1, Stderr: "ERROR error connecting to repository: repository already exists",
		},
	})
	res, err := moduleKopiaRepository(context.Background(), conn, map[string]any{
		"state":    "created",
		"password": "secret",
		"backend": map[string]any{
			"provider": "filesystem",
			"path":     "/mnt/backup/kopia",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("want the 'already exists' failure ignored: res = %+v", res)
	}
	if res.Changed {
		t.Fatalf("status text is unchanged before/after, want unchanged: res = %+v", res)
	}
}

func TestModuleKopiaRepositoryGenuineFailure(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"kopia repository status": {RC: 1, Stdout: ""},
		"kopia repository create filesystem --path=/mnt/backup/kopia --password=secret": {
			RC: 1, Stderr: "ERROR unable to open storage",
		},
	})
	res, err := moduleKopiaRepository(context.Background(), conn, map[string]any{
		"state":    "created",
		"password": "secret",
		"backend": map[string]any{
			"provider": "filesystem",
			"path":     "/mnt/backup/kopia",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed for a genuine (non-'already exists') error")
	}
}

func TestModuleKopiaRepositoryDisconnect(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"kopia repository status --config-file=/etc/kopia/root.config":     {RC: 1, Stdout: ""},
		"kopia repository disconnect --config-file=/etc/kopia/root.config": {RC: 0, Stdout: ""},
	})
	res, err := moduleKopiaRepository(context.Background(), conn, map[string]any{
		"state":  "disconnected",
		"config": "/etc/kopia/root.config",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v, commands = %v", res, conn.Commands)
	}
}

func TestModuleKopiaRepositoryThrottle(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"kopia repository status --config-file=/etc/kopia/root.config":                                                                 {RC: 0, Stdout: "same\n"},
		"kopia repository throttle set --download-bytes-per-second 0 --upload-bytes-per-second 0 --config-file=/etc/kopia/root.config": {RC: 0, Stdout: ""},
	})
	res, err := moduleKopiaRepository(context.Background(), conn, map[string]any{
		"state":  "throttled",
		"config": "/etc/kopia/root.config",
		"throttle": map[string]any{
			"download_bytes_per_second": 0,
			"upload_bytes_per_second":   0,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v, commands = %v", res, conn.Commands)
	}
	if res.Changed {
		t.Fatal("status text identical before/after, want unchanged")
	}
}

func TestModuleKopiaRepositoryThrottleFailsOnAnyError(t *testing.T) {
	// See moduleKopiaRepository's own doc comment: unlike real
	// state_throttled (which never raises, an apparent upstream
	// oversight), this port DOES fail state=throttled on a genuine
	// error.
	conn := newFakeConn(map[string]remoteexec.Result{
		"kopia repository status --config-file=/etc/kopia/root.config":       {RC: 0, Stdout: ""},
		"kopia repository throttle set --config-file=/etc/kopia/root.config": {RC: 1, Stderr: "ERROR not connected to a repository"},
	})
	res, err := moduleKopiaRepository(context.Background(), conn, map[string]any{
		"state":  "throttled",
		"config": "/etc/kopia/root.config",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed for a genuine throttle error")
	}
}

func TestModuleKopiaRepositoryConnectServerProviderOmitsToken(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"kopia repository status --config-file=/etc/kopia/root.config":                                                                                          {RC: 1, Stdout: ""},
		"kopia repository connect --password=secret --server-cert-fingerprint=AA:BB --url=https://kopia.example.com:51515 --config-file=/etc/kopia/root.config": {RC: 0, Stdout: ""},
	})
	res, err := moduleKopiaRepository(context.Background(), conn, map[string]any{
		"state":           "connected",
		"password":        "secret",
		"config":          "/etc/kopia/root.config",
		"url":             "https://kopia.example.com:51515",
		"fingerprint_tls": "AA:BB",
		"backend":         map[string]any{"provider": "server"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v, commands = %v", res, conn.Commands)
	}
}

func TestModuleKopiaRepositoryMissingBackend(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"kopia repository status": {RC: 1, Stdout: ""},
	})
	if _, err := moduleKopiaRepository(context.Background(), conn, map[string]any{"state": "created"}); err == nil {
		t.Fatal("want error for missing backend when state=created")
	}
}

func TestModuleKopiaRepositoryMissingBackendField(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"kopia repository status": {RC: 1, Stdout: ""},
	})
	if _, err := moduleKopiaRepository(context.Background(), conn, map[string]any{
		"state":   "created",
		"backend": map[string]any{"provider": "s3"},
	}); err == nil {
		t.Fatal("want error for a provider missing its required fields")
	}
}

func TestModuleKopiaRepositoryBadState(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleKopiaRepository(context.Background(), conn, map[string]any{"state": "bogus"}); err == nil {
		t.Fatal("want error for bad state")
	}
}
