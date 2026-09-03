package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleVerticaConfigurationChanged(t *testing.T) {
	getQuery := "select parameter_name, current_value from configuration_parameters where node_name = 'ALL' and parameter_name ilike " +
		verticaQuoteLiteral("failovertostandbyafter")
	setQuery := "select set_config_parameter(" + verticaQuoteLiteral("failovertostandbyafter") +
		", " + verticaQuoteLiteral("8 hours") + ")"

	conn := newFakeConn(map[string]remoteexec.Result{
		"vsql -h localhost -p 5433 -U dbadmin -X -A -t -c " + shellQuote(getQuery): {RC: 0, Stdout: "failovertostandbyafter|1 hour\n"},
		"vsql -h localhost -p 5433 -U dbadmin -X -A -t -c " + shellQuote(setQuery): {RC: 0},
	})
	res, err := moduleVerticaConfiguration(context.Background(), conn, map[string]any{
		"parameter": "failovertostandbyafter", "value": "8 hours",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleVerticaConfigurationUnchanged(t *testing.T) {
	getQuery := "select parameter_name, current_value from configuration_parameters where node_name = 'ALL' and parameter_name ilike " +
		verticaQuoteLiteral("failovertostandbyafter")
	conn := newFakeConn(map[string]remoteexec.Result{
		"vsql -h localhost -p 5433 -U dbadmin -X -A -t -c " + shellQuote(getQuery): {RC: 0, Stdout: "failovertostandbyafter|8 hours\n"},
	})
	res, err := moduleVerticaConfiguration(context.Background(), conn, map[string]any{
		"parameter": "failovertostandbyafter", "value": "8 hours",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleVerticaConfigurationUnknownParameter(t *testing.T) {
	getQuery := "select parameter_name, current_value from configuration_parameters where node_name = 'ALL' and parameter_name ilike " +
		verticaQuoteLiteral("bogus")
	conn := newFakeConn(map[string]remoteexec.Result{
		"vsql -h localhost -p 5433 -U dbadmin -X -A -t -c " + shellQuote(getQuery): {RC: 0, Stdout: ""},
	})
	res, err := moduleVerticaConfiguration(context.Background(), conn, map[string]any{
		"parameter": "bogus", "value": "x",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed for unknown parameter")
	}
}

func TestModuleVerticaConfigurationMissingParameter(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleVerticaConfiguration(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing parameter")
	}
}
