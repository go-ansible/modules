package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func asadmCmdKey(cmd string) string {
	return `asadm -h localhost -p 3000 --timeout 1 --no-color -e 'asinfo -v '"'"'` + cmd + `'"'"''`
}

func TestModuleAerospikeMigrationsClusterStableOK(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v asadm": {RC: 0},
		asadmCmdKey("build"): {
			RC: 0, Stdout: "5.0.0\n",
		},
		asadmCmdKey("statistics"): {
			RC: 0, Stdout: "cluster_key=abc123;cluster_size=3;migrate_allowed=true\n",
		},
		asadmCmdKey("cluster-stable:"): {
			RC: 0, Stdout: "abc123\n",
		},
	})
	args := map[string]any{
		"local_only":              true,
		"consecutive_good_checks": 1,
		"sleep_between_checks":    0,
		"tries_limit":             5,
		"min_cluster_size":        1,
		"fail_on_cluster_change":  true,
	}
	res, err := moduleAerospikeMigrations(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Changed {
		t.Fatal("want Changed=false, matching real module's own RETURN")
	}
}

func TestModuleAerospikeMigrationsLegacyBuildNoMigrations(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v asadm": {RC: 0},
		asadmCmdKey("build"): {
			RC: 0, Stdout: "3.16.0\n",
		},
		asadmCmdKey("statistics"): {
			RC: 0, Stdout: "cluster_key=abc123;cluster_size=1;migrate_allowed=true\n",
		},
		asadmCmdKey("namespaces"): {
			RC: 0, Stdout: "test\n",
		},
		asadmCmdKey("namespace/test"): {
			RC: 0, Stdout: "migrate_tx_partitions_remaining=0;migrate_rx_partitions_remaining=0\n",
		},
	})
	args := map[string]any{
		"local_only":              true,
		"consecutive_good_checks": 1,
		"sleep_between_checks":    0,
		"tries_limit":             5,
	}
	res, err := moduleAerospikeMigrations(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleAerospikeMigrationsFailsWhenExhausted(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v asadm": {RC: 0},
		asadmCmdKey("build"): {
			RC: 0, Stdout: "5.0.0\n",
		},
		asadmCmdKey("statistics"): {
			RC: 0, Stdout: "cluster_key=abc123;cluster_size=1;migrate_allowed=true\n",
		},
		asadmCmdKey("cluster-stable:"): {
			RC: 1, Stderr: "unstable-cluster",
		},
	})
	args := map[string]any{
		"local_only":              true,
		"consecutive_good_checks": 1,
		"sleep_between_checks":    0,
		"tries_limit":             2,
	}
	res, err := moduleAerospikeMigrations(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want Failed, res = %+v", res)
	}
}

func TestModuleAerospikeMigrationsMissingLocalOnly(t *testing.T) {
	conn := newFakeConn(nil)
	_, err := moduleAerospikeMigrations(context.Background(), conn, map[string]any{})
	if err == nil {
		t.Fatal("want error for missing local_only")
	}
}

func TestAsadmCanUseClusterStable(t *testing.T) {
	cases := map[string]bool{
		"3.16.0": false,
		"4.0.0":  false,
		"4.2.9":  false,
		"4.3.0":  true,
		"5.0.0":  true,
	}
	for build, want := range cases {
		if got := asadmCanUseClusterStable(build); got != want {
			t.Errorf("asadmCanUseClusterStable(%q) = %v, want %v", build, got, want)
		}
	}
}
