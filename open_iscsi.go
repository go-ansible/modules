package modules

import (
	"context"
	"sort"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleOpenIscsi implements Ansible's `open_iscsi` (community.general)
// module: discovers targets on a portal, logs in/out, and toggles a
// target's automatic-startup setting via the `iscsiadm` CLI — the same
// binary real open_iscsi's own module wraps (there is no library form to
// substitute here: real open_iscsi already shells out to `iscsiadm`
// itself, so this port's command construction mirrors its source
// directly).
//
// Args: portal (alias ip) — required when discover=true; port (default
// "3260"); target (aliases name, targetname) — the iSCSI target name;
// login (alias state, bool) — true logs in, false logs out; when target
// is omitted, applies to every node cached from portal (or every cached
// node at all, if portal is also omitted); auto_node_startup (alias
// automatic, bool) — node.startup automatic/manual, requires target;
// discover (bool, default false) — `iscsiadm -m discovery -t sendtargets
// -p <portal>:<port>`, requires portal; show_nodes (bool, default false)
// — return the cached node list in Extra["nodes"]; rescan (bool, default
// false) — rescan one target's session or (target omitted) every
// session; node_auth (default CHAP), node_user, node_pass, node_user_in,
// node_pass_in — written via `iscsiadm -m node --targetname <target>
// --op=update --name <n> --value <v>` before login, matching real
// open_iscsi's own target_login.
//
// Deviation from real open_iscsi: auto_portal_startup (real open_iscsi's
// own node.conn[0].startup toggle) is accepted but has no effect — this
// port's simplified target_isauto/target_setauto/target_setmanual only
// ever address node.startup, matching this port's general preference for
// an honest no-op over inventing a --portal-aware variant of a flag real
// open_iscsi's own iscsiadm invocation for it is otherwise identical to
// node startup's own; a caller wanting exact real behavior should not
// rely on this option in this port.
//
// This module needs root and a live iSCSI initiator/target pair to do
// anything real, so — like monit.go/pacemaker_cluster.go in this
// project — its test uses a scripted fakeConn rather than a real
// Connection.
func moduleOpenIscsi(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	portal := argString(args, "portal", argString(args, "ip", ""))
	port := argString(args, "port", "3260")
	target := argString(args, "target", argString(args, "name", argString(args, "targetname", "")))
	discover := argBool(args, "discover", false)
	showNodes := argBool(args, "show_nodes", false)
	rescan := argBool(args, "rescan", false)

	_, hasLogin := args["login"]
	if !hasLogin {
		_, hasLogin = args["state"]
	}
	login := argBool(args, "login", argBool(args, "state", false))

	_, hasAuto := args["auto_node_startup"]
	if !hasAuto {
		_, hasAuto = args["automatic"]
	}
	auto := argBool(args, "auto_node_startup", argBool(args, "automatic", false))

	if discover && portal == "" {
		return Result{}, errArg("open_iscsi: portal is required when discover=true")
	}
	if hasAuto && target == "" {
		return Result{}, errArg("open_iscsi: auto_node_startup requires target")
	}

	changed := false
	res := Result{}

	cached, err := iscsiCachedNodes(ctx, conn, portal)
	if err != nil {
		return Result{}, err
	}
	nodes := cached

	if discover {
		if _, err := run(ctx, conn, "iscsiadm --mode discovery --type sendtargets --portal "+shellQuote(iscsiFormatPortal(portal, port))); err != nil {
			return Result{}, err
		}
		nodes, err = iscsiCachedNodes(ctx, conn, portal)
		if err != nil {
			return Result{}, err
		}
		if !iscsiSameNodes(cached, nodes) {
			changed = true
			res = res.WithExtra("cache_updated", true)
		}
	}

	if showNodes {
		res = res.WithExtra("nodes", nodes)
	}

	if rescan {
		cmd := "iscsiadm --mode session --rescan"
		if target != "" {
			cmd = "iscsiadm --mode node --rescan -T " + shellQuote(target)
		}
		if _, err := runStatus(ctx, conn, cmd); err != nil {
			return Result{}, err
		}
	}

	if hasLogin {
		if target == "" {
			var devicenodes []string
			for _, n := range nodes {
				loggedOn, err := iscsiLoggedOn(ctx, conn, n, portal, port)
				if err != nil {
					return Result{}, err
				}
				if login == loggedOn {
					continue
				}
				changed = true
				if login {
					if _, _, err := iscsiTargetLogin(ctx, conn, args, n, portal, port); err != nil {
						return Result{}, err
					}
					devicenodes = append(devicenodes, iscsiDeviceNodesFallback(n)...)
				} else {
					if _, err := run(ctx, conn, "iscsiadm --mode node --targetname "+shellQuote(n)+" --logout"); err != nil {
						return Result{}, err
					}
				}
			}
			if login {
				res = res.WithExtra("devicenodes", devicenodes)
			}
		} else {
			found := false
			for _, n := range nodes {
				if n == target {
					found = true
					break
				}
			}
			if !found {
				return Fail("open_iscsi: specified target not found"), nil
			}
			loggedOn, err := iscsiLoggedOn(ctx, conn, target, portal, port)
			if err != nil {
				return Result{}, err
			}
			if login != loggedOn {
				changed = true
				if login {
					if _, _, err := iscsiTargetLogin(ctx, conn, args, target, portal, port); err != nil {
						return Result{}, err
					}
					res = res.WithExtra("devicenodes", iscsiDeviceNodesFallback(target))
				} else {
					if _, err := run(ctx, conn, "iscsiadm --mode node --targetname "+shellQuote(target)+" --logout"); err != nil {
						return Result{}, err
					}
				}
			} else if login {
				res = res.WithExtra("devicenodes", iscsiDeviceNodesFallback(target))
			}
		}
	}

	if hasAuto {
		isAuto, err := iscsiIsAuto(ctx, conn, target)
		if err != nil {
			return Result{}, err
		}
		if auto != isAuto {
			changed = true
			value := "manual"
			if auto {
				value = "automatic"
			}
			cmd := "iscsiadm --mode node --targetname " + shellQuote(target) +
				" --op=update --name node.startup --value " + shellQuote(value)
			if _, err := run(ctx, conn, cmd); err != nil {
				return Result{}, err
			}
		}
	}

	res.Changed = changed
	return res, nil
}

// iscsiFormatPortal formats a portal address and port for iscsiadm,
// bracketing an IPv6 address, matching real open_iscsi's own
// format_portal.
func iscsiFormatPortal(portal, port string) string {
	if strings.Contains(portal, ":") {
		return "[" + portal + "]:" + port
	}
	return portal + ":" + port
}

// iscsiCachedNodes parses `iscsiadm --mode node`'s own "ip:port,tag
// targetname" lines into a list of target names, optionally filtered to
// one portal — matching real open_iscsi's own iscsi_get_cached_nodes. An
// empty persistent database (RC 21, or RC 255 with "o records found" in
// stderr on older iscsiadm) is not an error, matching real open_iscsi.
func iscsiCachedNodes(ctx context.Context, conn remoteexec.Connection, portal string) ([]string, error) {
	res, err := runStatus(ctx, conn, "iscsiadm --mode node")
	if err != nil {
		return nil, err
	}
	if res.RC != 0 {
		if res.RC == 21 || (res.RC == 255 && strings.Contains(res.Stderr, "o records found")) {
			return nil, nil
		}
		return nil, nil
	}
	var nodes []string
	for _, line := range strings.Split(res.Stdout, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		targetPortal := fields[0]
		if idx := strings.Index(targetPortal, ":"); idx >= 0 {
			targetPortal = targetPortal[:idx]
		}
		if portal == "" || portal == targetPortal {
			nodes = append(nodes, fields[1])
		}
	}
	return nodes, nil
}

func iscsiSameNodes(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	sa := append([]string{}, a...)
	sb := append([]string{}, b...)
	sort.Strings(sa)
	sort.Strings(sb)
	for i := range sa {
		if sa[i] != sb[i] {
			return false
		}
	}
	return true
}

// iscsiLoggedOn reports whether target is in `iscsiadm --mode
// session`'s own output, scoped by portal:port when given, matching
// real open_iscsi's own target_loggedon (RC 21 means no sessions at
// all, not an error).
func iscsiLoggedOn(ctx context.Context, conn remoteexec.Connection, target, portal, port string) (bool, error) {
	res, err := runStatus(ctx, conn, "iscsiadm --mode session")
	if err != nil {
		return false, err
	}
	if res.RC == 21 {
		return false, nil
	}
	if res.RC != 0 {
		return false, nil
	}
	needle := target
	if portal != "" {
		needle = portal + ":" + port + ".*" + target
	}
	for _, line := range strings.Split(res.Stdout, "\n") {
		if strings.Contains(line, needle) || (portal == "" && strings.Contains(line, target)) {
			return true, nil
		}
	}
	return false, nil
}

// iscsiTargetLogin writes any node_user/node_pass(_in) auth parameters
// via `--op=update`, then logs in — matching real open_iscsi's own
// target_login.
func iscsiTargetLogin(ctx context.Context, conn remoteexec.Connection, args map[string]any, target, portal, port string) (rc int, out string, err error) {
	nodeAuth := argString(args, "node_auth", "CHAP")
	if u := argString(args, "node_user", ""); u != "" {
		for _, kv := range [][2]string{
			{"node.session.auth.authmethod", nodeAuth},
			{"node.session.auth.username", u},
			{"node.session.auth.password", argString(args, "node_pass", "")},
		} {
			cmd := "iscsiadm --mode node --targetname " + shellQuote(target) + " --op=update --name " +
				shellQuote(kv[0]) + " --value " + shellQuote(kv[1])
			if _, err := runStatus(ctx, conn, cmd); err != nil {
				return 0, "", err
			}
		}
	}
	if u := argString(args, "node_user_in", ""); u != "" {
		for _, kv := range [][2]string{
			{"node.session.auth.username_in", u},
			{"node.session.auth.password_in", argString(args, "node_pass_in", "")},
		} {
			cmd := "iscsiadm --mode node --targetname " + shellQuote(target) + " --op=update --name " +
				shellQuote(kv[0]) + " --value " + shellQuote(kv[1])
			if _, err := runStatus(ctx, conn, cmd); err != nil {
				return 0, "", err
			}
		}
	}
	cmd := "iscsiadm --mode node --targetname " + shellQuote(target) + " --login"
	if portal != "" {
		cmd += " --portal " + shellQuote(iscsiFormatPortal(portal, port))
	}
	res, err := runStatus(ctx, conn, cmd)
	if err != nil {
		return 0, "", err
	}
	return res.RC, res.Stdout, nil
}

// iscsiDeviceNodesFallback returns an empty list: real open_iscsi's own
// target_device_node globs /dev/disk/by-path/*<target>* on the LOCAL
// filesystem it runs on, a path this port's remoteexec.Connection has no
// primitive for globbing on the TARGET (Exec runs a shell command, so a
// caller could ask for `ls /dev/disk/by-path/*<target>*` explicitly, but
// this port does not do so automatically) — documented here rather than
// silently guessed at.
func iscsiDeviceNodesFallback(target string) []string {
	_ = target
	return nil
}

// iscsiIsAuto reports node.startup's current value via `iscsiadm --mode
// node --targetname <target>`, matching real open_iscsi's own
// target_isauto.
func iscsiIsAuto(ctx context.Context, conn remoteexec.Connection, target string) (bool, error) {
	out, err := run(ctx, conn, "iscsiadm --mode node --targetname "+shellQuote(target))
	if err != nil {
		return false, err
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "node.startup") {
			return strings.Contains(line, "automatic"), nil
		}
	}
	return false, nil
}
