package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestRegistryDefault(t *testing.T) {
	r := Default()
	want := []string{
		"acl", "alternatives", "apk", "apt", "apt_key", "apt_repository",
		"archive", "assemble", "assert", "async_status", "at", "authorized_key",
		"blockinfile", "btrfs_info", "btrfs_subvolume", "bundler",
		"capabilities", "cargo", "cloud_init_data_facts", "command", "composer",
		"consul", "consul_acl_bootstrap", "consul_agent_check",
		"consul_agent_service", "consul_auth_method", "consul_binding_rule",
		"consul_kv", "consul_kv_info", "consul_policy", "consul_role",
		"consul_session", "consul_token", "copy", "cpanm", "cron", "cronvar",
		"crypttab", "dconf", "deb822_repository", "debconf", "debug",
		"decompress", "deploy_helper", "django_command", "django_manage", "dnf",
		"dnf5", "dnf_config_manager", "dnf_versionlock", "dpkg_selections",
		"expect", "facter_facts", "fail", "fetch", "file", "filesize",
		"filesystem", "find", "firewalld", "firewalld_info", "flatpak",
		"flatpak_remote", "gather_facts", "gem", "get_url", "getent",
		"gio_mime", "git", "git_config", "git_config_info", "golang_package",
		"group", "haproxy", "homebrew", "homebrew_cask", "homebrew_services",
		"homebrew_tap", "homectl", "hostname", "htpasswd", "ini_file",
		"installp", "interfaces_file", "ipa_config", "ipa_dnsrecord",
		"ipa_dnszone", "ipa_getkeytab", "ipa_group", "ipa_hbacrule", "ipa_host",
		"ipa_hostgroup", "ipa_otpconfig", "ipa_otptoken", "ipa_pwpolicy",
		"ipa_role", "ipa_service", "ipa_subca", "ipa_sudocmd",
		"ipa_sudocmdgroup", "ipa_sudorule", "ipa_user", "ipa_vault", "iptables",
		"iptables_state", "java_cert", "java_keystore", "kdeconfig",
		"kernel_blacklist", "known_hosts", "krb_ticket", "launchd",
		"ldap_attrs", "ldap_entry", "ldap_passwd", "ldap_search", "lineinfile",
		"listen_ports_facts", "lldp_facts", "locale_gen", "logrotate", "lvg",
		"lvg_rename", "lvm_pv", "lvm_pv_move_data", "lvol", "lxc_container",
		"lxd_container", "lxd_profile", "lxd_project", "lxd_storage_pool_info",
		"lxd_storage_volume_info", "macports", "mail", "mas", "maven_artifact",
		"modprobe", "monit", "mount", "mount_facts", "mssql_db", "mssql_script",
		"nagios", "nginx_status_info", "nmcli", "nomad_job", "nomad_job_info",
		"nomad_token", "npm", "nsupdate", "ohai", "open_iscsi", "openbsd_pkg",
		"opkg", "osx_defaults", "pacemaker_cluster", "pacemaker_info",
		"pacemaker_resource", "package", "package_facts", "pacman",
		"pacman_key", "pam_limits", "pamd", "parted", "patch", "pause", "pear",
		"pids", "ping", "pip", "pip_package_info", "pipx", "pipx_info", "pkg5",
		"pkg5_publisher", "pkgin", "pkgng", "pkgutil", "pnpm", "portage",
		"portinstall", "puppet", "python_requirements_info", "raw", "read_csv",
		"reboot", "redhat_subscription", "redis", "redis_data",
		"redis_data_incr", "redis_data_info", "redis_info", "replace",
		"rhel_facts", "rhel_rpm_ostree", "rhsm_release", "rhsm_repository",
		"rpm_key", "rpm_ostree_upgrade", "runit", "script", "seboolean",
		"sefcontext", "selinux", "selinux_permissive", "selogin", "seport",
		"service", "service_facts", "set_fact", "set_stats", "setup", "shell",
		"slackpkg", "slurp", "snap", "snap_alias", "snmp_facts", "ssh_config",
		"sssd_info", "stat", "subversion", "sudoers", "supervisorctl",
		"svr4pkg", "synchronize", "sysctl", "syspatch", "sysrc", "systemd",
		"systemd_creds_decrypt", "systemd_creds_encrypt", "systemd_info",
		"systemd_service", "sysvinit", "tempfile", "template", "terraform",
		"timezone", "ufw", "unarchive", "uri", "usb_facts", "user", "uv_python",
		"validate_argument_spec", "vdo", "vertica_configuration",
		"vertica_info", "vertica_role", "vertica_schema", "vertica_user",
		"wait_for", "wait_for_connection", "wakeonlan", "xattr", "xbps",
		"xdg_mime", "xfconf", "xfconf_info", "xfs_quota", "xml", "xml_info",
		"yarn", "yum_repository", "yum_versionlock", "zfs",
		"zfs_delegate_admin", "zfs_facts", "znode", "zpool", "zpool_facts",
		"zypper", "zypper_repository", "zypper_repository_info",
	}
	names := r.Names()
	if len(names) != len(want) {
		t.Fatalf("Names() = %v, want %d entries", names, len(want))
	}
	for _, w := range want {
		if _, ok := r.Get(w); !ok {
			t.Errorf("module %q not registered", w)
		}
	}
}

func TestRegistryRunUnknownModule(t *testing.T) {
	r := Default()
	res, err := r.Run(context.Background(), "no_such_module", local(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed for an unknown module name")
	}
}

func TestRegistryRunKnownModule(t *testing.T) {
	r := Default()
	res, err := r.Run(context.Background(), "debug", local(), map[string]any{"msg": "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Msg != "hi" {
		t.Fatalf("Msg = %q", res.Msg)
	}
}

func TestRegistryOverride(t *testing.T) {
	r := NewRegistry()
	r.Register("x", func(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
		return Ok("first"), nil
	})
	r.Register("x", func(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
		return Ok("second"), nil
	})
	res, err := r.Run(context.Background(), "x", local(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Msg != "second" {
		t.Fatalf("Msg = %q, want the later Register to win", res.Msg)
	}
}

func TestResultWithExtra(t *testing.T) {
	r := Ok("m").WithExtra("a", 1).WithExtra("b", 2)
	if r.Extra["a"] != 1 || r.Extra["b"] != 2 {
		t.Fatalf("Extra = %v", r.Extra)
	}
}

func TestFailHelper(t *testing.T) {
	r := Fail("boom")
	if !r.Failed || r.Msg != "boom" {
		t.Fatalf("r = %+v", r)
	}
}

func TestChangedHelper(t *testing.T) {
	r := Changed("did it")
	if !r.Changed || r.Msg != "did it" {
		t.Fatalf("r = %+v", r)
	}
}
