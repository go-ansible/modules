package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleIcinga2FeatureEnable(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"LANGUAGE=C LC_ALL=C icinga2 feature list": {
			RC: 0, Stdout: "Disabled features: api ido-pgsql\nEnabled features: checker mainlog notification\n",
		},
		"LANGUAGE=C LC_ALL=C icinga2 feature enable ido-pgsql": {
			RC: 0, Stdout: "Enabling feature ido-pgsql. Make sure to restart Icinga 2.\n",
		},
	})
	res, err := moduleIcinga2Feature(context.Background(), conn, map[string]any{"name": "ido-pgsql"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v, commands = %v", res, conn.Commands)
	}
}

func TestModuleIcinga2FeatureAlreadyEnabledSkips(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"LANGUAGE=C LC_ALL=C icinga2 feature list": {
			RC: 0, Stdout: "Disabled features: api\nEnabled features: checker ido-pgsql mainlog\n",
		},
	})
	res, err := moduleIcinga2Feature(context.Background(), conn, map[string]any{"name": "ido-pgsql"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v, commands = %v", res, conn.Commands)
	}
}

func TestModuleIcinga2FeatureDisable(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"LANGUAGE=C LC_ALL=C icinga2 feature list": {
			RC: 0, Stdout: "Disabled features: ido-pgsql\nEnabled features: api checker mainlog\n",
		},
		"LANGUAGE=C LC_ALL=C icinga2 feature disable api": {
			RC: 0, Stdout: "Disabling feature api. Make sure to restart Icinga 2.\n",
		},
	})
	res, err := moduleIcinga2Feature(context.Background(), conn, map[string]any{"name": "api", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v, commands = %v", res, conn.Commands)
	}
}

func TestModuleIcinga2FeatureDisableAlreadyDisabled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"LANGUAGE=C LC_ALL=C icinga2 feature list": {
			RC: 0, Stdout: "Disabled features: api\nEnabled features: checker mainlog\n",
		},
	})
	res, err := moduleIcinga2Feature(context.Background(), conn, map[string]any{"name": "api", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v, commands = %v", res, conn.Commands)
	}
}

func TestModuleIcinga2FeatureDisableRaceAlreadyGone(t *testing.T) {
	// The list check says "api" is enabled, but by the time `disable`
	// runs it's already gone (target file missing) — real
	// icinga2_feature treats this as unchanged, not a failure.
	conn := newFakeConn(map[string]remoteexec.Result{
		"LANGUAGE=C LC_ALL=C icinga2 feature list": {
			RC: 0, Stdout: "Disabled features: \nEnabled features: api checker mainlog\n",
		},
		"LANGUAGE=C LC_ALL=C icinga2 feature disable api": {
			RC: 1, Stdout: "Cannot disable feature 'api'. Target file '/etc/icinga2/features-enabled/api.conf' does not exist.\n",
		},
	})
	res, err := moduleIcinga2Feature(context.Background(), conn, map[string]any{"name": "api", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v, commands = %v", res, conn.Commands)
	}
}

func TestModuleIcinga2FeatureListFails(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"LANGUAGE=C LC_ALL=C icinga2 feature list": {RC: 1, Stderr: "command not found"},
	})
	res, err := moduleIcinga2Feature(context.Background(), conn, map[string]any{"name": "api"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed when icinga2 feature list fails")
	}
}

func TestModuleIcinga2FeatureMissingName(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleIcinga2Feature(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing name")
	}
}
