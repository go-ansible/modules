package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

const pipxEmptyList = `{"pipx_spec_version": "0.1", "venvs": {}}`

const pipxToxList = `{"venvs": {"tox": {"metadata": {"main_package": {"package_version": "3.24.0", "pinned": false, "app_paths_of_dependencies": {"virtualenv": {}}}, "injected_packages": {}}}}}`

func TestModulePipxInstall(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pipx version": {RC: 0, Stdout: "1.7.1\n"},
		"USE_EMOJI=0 PIPX_USE_EMOJI=0 pipx list --include-injected --json": {RC: 0, Stdout: pipxEmptyList},
		"pipx install tox": {RC: 0},
	})
	res, err := modulePipx(context.Background(), conn, map[string]any{"name": "tox"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}

func TestModulePipxInstallAlreadyPresent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pipx version": {RC: 0, Stdout: "1.7.1\n"},
		"USE_EMOJI=0 PIPX_USE_EMOJI=0 pipx list --include-injected --json": {RC: 0, Stdout: pipxToxList},
	})
	res, err := modulePipx(context.Background(), conn, map[string]any{"name": "tox", "state": "present"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModulePipxUninstall(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pipx version": {RC: 0, Stdout: "1.7.1\n"},
		"USE_EMOJI=0 PIPX_USE_EMOJI=0 pipx list --include-injected --json": {RC: 0, Stdout: pipxToxList},
		"pipx uninstall tox": {RC: 0},
	})
	res, err := modulePipx(context.Background(), conn, map[string]any{"name": "tox", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModulePipxUpgradeNonExistent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pipx version": {RC: 0, Stdout: "1.7.1\n"},
		"USE_EMOJI=0 PIPX_USE_EMOJI=0 pipx list --include-injected --json": {RC: 0, Stdout: pipxEmptyList},
	})
	res, err := modulePipx(context.Background(), conn, map[string]any{"name": "tox", "state": "upgrade"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed for upgrading a non-existent application")
	}
}

func TestModulePipxUpgrade(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pipx version": {RC: 0, Stdout: "1.7.1\n"},
		"USE_EMOJI=0 PIPX_USE_EMOJI=0 pipx list --include-injected --json": {RC: 0, Stdout: pipxToxList},
		"pipx upgrade tox": {RC: 0},
	})
	res, err := modulePipx(context.Background(), conn, map[string]any{"name": "tox", "state": "upgrade"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModulePipxInjectRequiresPackages(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pipx version": {RC: 0, Stdout: "1.7.1\n"},
	})
	if _, err := modulePipx(context.Background(), conn, map[string]any{"name": "tox", "state": "inject"}); err == nil {
		t.Fatal("want error: inject_packages required")
	}
}

func TestModulePipxInject(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pipx version": {RC: 0, Stdout: "1.7.1\n"},
		"USE_EMOJI=0 PIPX_USE_EMOJI=0 pipx list --include-injected --json": {RC: 0, Stdout: pipxToxList},
		"pipx inject tox pytest-cov":                                       {RC: 0},
	})
	res, err := modulePipx(context.Background(), conn, map[string]any{
		"name": "tox", "state": "inject", "inject_packages": []string{"pytest-cov"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModulePipxGlobalFlag(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pipx --global version": {RC: 0, Stdout: "1.7.1\n"},
		"USE_EMOJI=0 PIPX_USE_EMOJI=0 pipx --global list --include-injected --json": {RC: 0, Stdout: pipxEmptyList},
		"pipx --global install tox": {RC: 0},
	})
	res, err := modulePipx(context.Background(), conn, map[string]any{"name": "tox", "global": true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModulePipxOldVersionRejected(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pipx version": {RC: 0, Stdout: "1.6.0\n"},
	})
	if _, err := modulePipx(context.Background(), conn, map[string]any{"name": "tox"}); err == nil {
		t.Fatal("want error for pipx < 1.7.0")
	}
}

func TestModulePipxUnsupportedState(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pipx version": {RC: 0, Stdout: "1.7.1\n"},
	})
	if _, err := modulePipx(context.Background(), conn, map[string]any{"name": "tox", "state": "pin"}); err == nil {
		t.Fatal("want error: state=pin is not supported")
	}
}

func TestModulePipxMissingName(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pipx version": {RC: 0, Stdout: "1.7.1\n"},
	})
	if _, err := modulePipx(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing name")
	}
}
