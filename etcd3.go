package modules

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleEtcd3 implements Ansible's `etcd3` (community.general) module:
// gets, sets, or deletes a key in an etcd v3 cluster.
//
// Architectural note: real etcd3 talks to the cluster using the Python
// `etcd3` gRPC client library, in-process. This port has no gRPC client
// and no access to that library, so — matching this batch's own
// assignment brief — it substitutes the `etcdctl` CLI (etcd's own
// official client binary) run on the target via `conn`, with
// `ETCDCTL_API=3` forced so an older etcdctl defaulting to the v2 API
// still speaks v3. Functionally equivalent (same cluster, same key
// space); architecturally different (a subprocess per call instead of a
// held gRPC channel) — documented rather than hidden.
//
// Args: key (string, required); value (string, required if
// state=present); state (present|absent, required); host (string,
// default "localhost"); port (int, default 2379); user/password
// (string, optional — password required if user given) — passed as
// etcdctl's own `--user=user:password`; ca_cert/client_cert/client_key
// (string paths, optional) — passed as `--cacert`/`--cert`/`--key`;
// presence of ca_cert also selects an `https://` endpoint scheme over
// plain `http://`; timeout (int, optional, seconds) — passed as
// `--command-timeout=Ns`.
//
// State semantics: present is idempotent against the key's current
// value (`etcdctl get --print-value-only`) — a matching value is a
// no-op; absent is idempotent against the key's mere existence. Both
// return `old_value` (the value before this task ran, "" if the key did
// not exist), matching real etcd3's own return contract.
func moduleEtcd3(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	key, err := requireString(args, "key")
	if err != nil {
		return Result{}, err
	}
	state, err := requireString(args, "state")
	if err != nil {
		return Result{}, err
	}
	if state != "present" && state != "absent" {
		return Result{}, errArg("etcd3: state must be present or absent, got %q", state)
	}

	flags, err := etcd3Flags(args)
	if err != nil {
		return Result{}, err
	}

	getRes, err := runStatus(ctx, conn, "ETCDCTL_API=3 etcdctl get "+shellQuote(key)+" --print-value-only"+flags)
	if err != nil {
		return Result{}, err
	}
	found := getRes.RC == 0
	oldValue := ""
	if found {
		oldValue = strings.TrimRight(getRes.Stdout, "\n")
	}

	switch state {
	case "present":
		value, err := requireString(args, "value")
		if err != nil {
			return Result{}, err
		}
		if found && oldValue == value {
			return Ok(key+" unchanged").WithExtra("key", key).WithExtra("old_value", oldValue), nil
		}
		if _, err := run(ctx, conn, "ETCDCTL_API=3 etcdctl put "+shellQuote(key)+" "+shellQuote(value)+flags); err != nil {
			return Result{}, err
		}
		return Changed(key+" set").WithExtra("key", key).WithExtra("old_value", oldValue), nil

	default: // absent
		if !found {
			return Ok(key+" already absent").WithExtra("key", key).WithExtra("old_value", ""), nil
		}
		if _, err := run(ctx, conn, "ETCDCTL_API=3 etcdctl del "+shellQuote(key)+flags); err != nil {
			return Result{}, err
		}
		return Changed(key+" deleted").WithExtra("key", key).WithExtra("old_value", oldValue), nil
	}
}

// etcd3Flags builds the shared etcdctl flag suffix (endpoints, auth,
// TLS, timeout) from this module's own arguments.
func etcd3Flags(args map[string]any) (string, error) {
	host := argString(args, "host", "localhost")
	port := argInt(args, "port", 2379)
	caCert := argString(args, "ca_cert", "")
	clientCert := argString(args, "client_cert", "")
	clientKey := argString(args, "client_key", "")
	user := argString(args, "user", "")
	password := argString(args, "password", "")
	timeout := argInt(args, "timeout", 0)

	scheme := "http"
	if caCert != "" || clientCert != "" {
		scheme = "https"
	}
	flags := " --endpoints=" + shellQuote(fmt.Sprintf("%s://%s:%d", scheme, host, port))

	if user != "" {
		if password == "" {
			return "", errArg("etcd3: password is required when user is set")
		}
		flags += " --user=" + shellQuote(user+":"+password)
	}
	if caCert != "" {
		flags += " --cacert=" + shellQuote(caCert)
	}
	if clientCert != "" {
		flags += " --cert=" + shellQuote(clientCert)
	}
	if clientKey != "" {
		flags += " --key=" + shellQuote(clientKey)
	}
	if timeout > 0 {
		flags += " --command-timeout=" + strconv.Itoa(timeout) + "s"
	}
	return flags, nil
}
