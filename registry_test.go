package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestRegistryDefault(t *testing.T) {
	r := Default()
	want := []string{
		"acl", "aix_devices", "aix_filesystem", "aix_inittab", "aix_lvg",
		"aix_lvol", "alternatives", "android_sdk", "apache2_mod_proxy",
		"apache2_module", "apk", "apt", "apt_key", "apt_repo", "apt_repository",
		"apt_rpm", "archive", "assemble", "assert", "async_status", "at",
		"authorized_key", "awall", "beadm", "blockinfile", "bootc_manage",
		"btrfs_info", "btrfs_subvolume", "bundler", "bzr", "capabilities",
		"cargo", "cloud_init_data_facts", "cobbler_sync", "cobbler_system",
		"command", "composer", "consul", "consul_acl_bootstrap",
		"consul_agent_check", "consul_agent_service", "consul_auth_method",
		"consul_binding_rule", "consul_kv", "consul_kv_info", "consul_policy",
		"consul_role", "consul_session", "consul_token", "copr", "copy",
		"cpanm", "cron", "cronvar", "crypttab", "dconf", "deb822_repository",
		"debconf", "debug", "decompress", "deploy_helper", "django_check",
		"django_command", "django_createcachetable", "django_dumpdata",
		"django_loaddata", "django_manage", "dnf", "dnf5", "dnf_config_manager",
		"dnf_versionlock", "dpkg_divert", "dpkg_selections", "ejabberd_user",
		"elasticsearch_plugin", "etcd3", "expect", "facter_facts", "fail",
		"fetch", "file", "filesize", "filesystem", "find", "firewalld",
		"firewalld_info", "flatpak", "flatpak_remote", "gather_facts",
		"gconftool2", "gconftool2_info", "gem", "get_url", "getent", "gio_mime",
		"git", "git_config", "git_config_info", "golang_package", "group",
		"gunicorn", "haproxy", "hg", "homebrew", "homebrew_cask",
		"homebrew_services", "homebrew_tap", "homectl", "hostname", "htpasswd",
		"icinga2_downtime", "icinga2_feature", "icinga2_host", "imgadm",
		"influxdb_database", "influxdb_query", "influxdb_retention_policy",
		"influxdb_user", "influxdb_write", "ini_file", "installp",
		"interfaces_file", "ip_netns", "ipa_config", "ipa_dnsrecord",
		"ipa_dnszone", "ipa_getkeytab", "ipa_group", "ipa_hbacrule", "ipa_host",
		"ipa_hostgroup", "ipa_otpconfig", "ipa_otptoken", "ipa_pwpolicy",
		"ipa_role", "ipa_service", "ipa_subca", "ipa_sudocmd",
		"ipa_sudocmdgroup", "ipa_sudorule", "ipa_user", "ipa_vault",
		"ipmi_boot", "ipmi_power", "iptables", "iptables_state", "iso_create",
		"iso_customize", "iso_extract", "java_cert", "java_keystore", "jboss",
		"kdeconfig", "kernel_blacklist", "kibana_plugin", "known_hosts",
		"kopia_repository", "kopia_repository_info", "krb_ticket", "launchd",
		"layman", "lbu", "ldap_attrs", "ldap_entry", "ldap_inc", "ldap_passwd",
		"ldap_search", "lineinfile", "listen_ports_facts", "lldp_facts",
		"locale_gen", "logrotate", "logstash_plugin", "lvg", "lvg_rename",
		"lvm_pv", "lvm_pv_move_data", "lvol", "lxc_container", "lxd_container",
		"lxd_profile", "lxd_project", "lxd_storage_pool_info",
		"lxd_storage_volume_info", "macports", "mail", "make", "mas",
		"maven_artifact", "mksysb", "modprobe", "monit", "mount", "mount_facts",
		"mqtt", "mssql_db", "mssql_script", "nagios", "nginx_status_info",
		"nictagadm", "nmcli", "nomad_job", "nomad_job_info", "nomad_token",
		"nosh", "npm", "nsupdate", "ohai", "omapi_host", "open_iscsi",
		"openbsd_pkg", "opendj_backendprop", "openwrt_init", "opkg",
		"osx_defaults", "pacemaker_cluster", "pacemaker_info",
		"pacemaker_resource", "package", "package_facts", "pacman",
		"pacman_key", "pam_limits", "pamd", "parted", "patch", "pause", "pear",
		"pids", "ping", "pip", "pip_package_info", "pipx", "pipx_info", "pkg5",
		"pkg5_publisher", "pkgin", "pkgng", "pkgutil", "pmem", "pnpm",
		"portage", "portinstall", "puppet", "python_requirements_info", "raw",
		"read_csv", "reboot", "redhat_subscription", "redis", "redis_data",
		"redis_data_incr", "redis_data_info", "redis_info", "replace",
		"rhel_facts", "rhel_rpm_ostree", "rhsm_release", "rhsm_repository",
		"rpm_key", "rpm_ostree_pkg", "rpm_ostree_upgrade", "runit", "script",
		"seboolean", "sefcontext", "selinux", "selinux_permissive", "selogin",
		"seport", "service", "service_facts", "set_fact", "set_stats", "setup",
		"shell", "shutdown", "simpleinit_msb", "slackpkg", "slurp",
		"smartos_image_info", "snap", "snap_alias", "snap_connect",
		"snmp_facts", "solaris_zone", "sorcery", "ssh_config", "sssd_info",
		"stat", "statsd", "subversion", "sudoers", "supervisorctl", "svc",
		"svr4pkg", "swdepot", "swupd", "synchronize", "sysctl", "syslogger",
		"syspatch", "sysrc", "systemd", "systemd_creds_decrypt",
		"systemd_creds_encrypt", "systemd_info", "systemd_service",
		"sysupgrade", "sysvinit", "tempfile", "template", "terraform",
		"timezone", "udm_dns_record", "udm_dns_zone", "udm_group", "udm_share",
		"udm_user", "ufw", "unarchive", "uri", "urpmi", "usb_facts", "user",
		"uv_python", "validate_argument_spec", "vdo", "vertica_configuration",
		"vertica_info", "vertica_role", "vertica_schema", "vertica_user",
		"wait_for", "wait_for_connection", "wakeonlan", "write_binary_file",
		"xattr", "xbps", "xdg_mime", "xenserver_facts", "xenserver_guest",
		"xenserver_guest_info", "xenserver_guest_powerstate", "xfconf",
		"xfconf_info", "xfs_quota", "xml", "xml_info", "yarn", "yum_repository",
		"yum_versionlock", "zfs", "zfs_delegate_admin", "zfs_facts", "znode",
		"zpool", "zpool_facts", "zypper", "zypper_repository",
		"zypper_repository_info",
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
