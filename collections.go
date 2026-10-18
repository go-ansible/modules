package modules

import (
	"strings"
	"sync"
)

// Collection reports which real Ansible collection a module name comes
// from: "ansible.builtin", "ansible.posix" or "community.general". An
// unregistered name, or one this table does not list, gives "".
//
// It exists because a consumer printing a fully-qualified name had no
// way to get it right. cli's ansible-doc assumed every module here was
// one of ansible-core's own and printed `ansible.builtin.<name>` for
// all of them -- true when it was written, and wrong for 514 of the 576
// registered today. Real ansible-doc prints
// `community.general.rocketchat`, and a user copying our header into a
// playbook got a name real Ansible rejects. This port's own
// NormalizeName strips any known prefix, so the wrong name worked HERE
// and nowhere else, which is the worst shape for that kind of mistake.
//
// # Where the lists come from
//
// Measured on 2026-10-18 against real ansible-core 2.21.4, by
// intersecting Default().Names() with
//
//	ansible-doc -l <collection> -t module
//
// for each of the three collections -- not assigned by hand and not
// inferred from a file's position in registry.go. The three sets are
// disjoint and their union is exactly the registry, both asserted by
// TestCollectionAttributionIsTotal, so a module added without an entry
// here fails the build rather than silently becoming ansible.builtin.
//
// 62 of ansible.builtin's 71 are here; the other nine (add_host,
// group_by, meta and the import_*/include_* family) are playbook
// constructs rather than remote modules, and live in go-ansible/playbook
// -- which is where real Ansible keeps them too.

// builtinModules are the ansible.builtin modules this registry holds.
var builtinModules = []string{
	"apt", "apt_key", "apt_repository", "assemble", "assert", "async_status",
	"blockinfile", "command", "copy", "cron", "deb822_repository", "debconf",
	"debug", "dnf", "dnf5", "dpkg_selections", "expect", "fail", "fetch",
	"file", "find", "gather_facts", "get_url", "getent", "git", "group",
	"hostname", "iptables", "known_hosts", "lineinfile", "mount_facts",
	"package", "package_facts", "pause", "ping", "pip", "raw", "reboot",
	"replace", "rpm_key", "script", "service", "service_facts", "set_fact",
	"set_stats", "setup", "shell", "slurp", "stat", "subversion", "systemd",
	"systemd_service", "sysvinit", "tempfile", "template", "unarchive", "uri",
	"user", "validate_argument_spec", "wait_for", "wait_for_connection",
	"yum_repository",
}

// posixModules are the ansible.posix modules this registry holds: all 14
// of them.
var posixModules = []string{
	"acl", "at", "authorized_key", "firewalld", "firewalld_info", "mount",
	"patch", "rhel_facts", "rhel_rpm_ostree", "rpm_ostree_upgrade",
	"seboolean", "selinux", "synchronize", "sysctl",
}

// communityGeneralModules are the community.general modules this
// registry holds -- 500 of that collection's 577.
var communityGeneralModules = []string{
	"aerospike_migrations", "airbrake_deployment", "aix_devices",
	"aix_filesystem", "aix_inittab", "aix_lvg", "aix_lvol", "alerta_customer",
	"ali_instance", "ali_instance_info", "alternatives", "android_sdk",
	"ansible_galaxy_install", "apache2_mod_proxy", "apache2_module", "apk",
	"apt_repo", "apt_rpm", "archive", "awall", "beadm", "bigpanda",
	"bootc_manage", "bower", "btrfs_info", "btrfs_subvolume", "bundler",
	"bzr", "campfire", "capabilities", "cargo", "circonus_annotation",
	"cisco_webex", "cloud_init_data_facts", "cloudflare_dns", "cobbler_sync",
	"cobbler_system", "composer", "consul", "consul_acl_bootstrap",
	"consul_agent_check", "consul_agent_service", "consul_auth_method",
	"consul_binding_rule", "consul_kv", "consul_kv_info", "consul_policy",
	"consul_role", "consul_session", "consul_token", "copr", "cpanm",
	"cronvar", "crypttab", "dconf", "decompress", "deploy_helper", "discord",
	"django_check", "django_command", "django_createcachetable",
	"django_dumpdata", "django_loaddata", "django_manage",
	"dnf_config_manager", "dnf_versionlock", "dnsimple", "dnsimple_info",
	"dpkg_divert", "easy_install", "ejabberd_user", "elasticsearch_plugin",
	"emc_vnx_sg_member", "etcd3", "facter_facts", "file_remove", "filesize",
	"filesystem", "flatpak", "flatpak_remote", "gconftool2",
	"gconftool2_info", "gem", "gio_mime", "git_config", "git_config_info",
	"github_deploy_key", "github_issue", "github_key", "github_release",
	"github_repo", "github_secrets", "github_secrets_info", "github_webhook",
	"github_webhook_info", "gitlab_branch", "gitlab_deploy_key",
	"gitlab_group", "gitlab_group_access_token", "gitlab_group_members",
	"gitlab_group_variable", "gitlab_hook", "gitlab_instance_variable",
	"gitlab_issue", "gitlab_label", "gitlab_merge_request",
	"gitlab_milestone", "gitlab_project", "gitlab_project_access_token",
	"gitlab_project_approvals", "gitlab_project_badge",
	"gitlab_project_members", "gitlab_project_variable",
	"gitlab_protected_branch", "gitlab_runner", "gitlab_user",
	"golang_package", "google_chat", "grove", "gunicorn", "haproxy",
	"heroku_collaborator", "hg", "homebrew", "homebrew_cask",
	"homebrew_services", "homebrew_tap", "homectl", "honeybadger_deployment",
	"hpilo_boot", "hpilo_info", "hponcfg", "htpasswd", "hwc_ecs_instance",
	"hwc_evs_disk", "hwc_network_vpc", "hwc_smn_topic", "hwc_vpc_eip",
	"hwc_vpc_peering_connect", "hwc_vpc_port", "hwc_vpc_private_ip",
	"hwc_vpc_route", "hwc_vpc_security_group", "hwc_vpc_security_group_rule",
	"hwc_vpc_subnet", "ibm_sa_domain", "ibm_sa_host", "ibm_sa_host_ports",
	"ibm_sa_pool", "ibm_sa_vol", "ibm_sa_vol_map", "icinga2_downtime",
	"icinga2_feature", "icinga2_host", "idrac_redfish_command",
	"idrac_redfish_config", "idrac_redfish_info", "ilo_redfish_command",
	"ilo_redfish_config", "ilo_redfish_info", "imgadm", "infinity",
	"influxdb_database", "influxdb_query", "influxdb_retention_policy",
	"influxdb_user", "influxdb_write", "ini_file", "installp",
	"interfaces_file", "ip2location_info", "ip_netns", "ipa_config",
	"ipa_dnsrecord", "ipa_dnszone", "ipa_getkeytab", "ipa_group",
	"ipa_hbacrule", "ipa_host", "ipa_hostgroup", "ipa_otpconfig",
	"ipa_otptoken", "ipa_pwpolicy", "ipa_role", "ipa_service", "ipa_subca",
	"ipa_sudocmd", "ipa_sudocmdgroup", "ipa_sudorule", "ipa_user",
	"ipa_vault", "ipinfoio_facts", "ipmi_boot", "ipmi_power",
	"iptables_state", "ipwcli_dns", "iso_create", "iso_customize",
	"iso_extract", "java_cert", "java_keystore", "jboss", "jenkins_build",
	"jenkins_build_info", "jenkins_credential", "jenkins_job",
	"jenkins_job_info", "jenkins_node", "jenkins_plugin", "jenkins_script",
	"jira", "kdeconfig", "kea_command", "kernel_blacklist",
	"keycloak_authentication", "keycloak_authentication_required_actions",
	"keycloak_authentication_v2", "keycloak_authz_authorization_scope",
	"keycloak_authz_custom_policy", "keycloak_authz_permission",
	"keycloak_authz_permission_info", "keycloak_client",
	"keycloak_client_rolemapping", "keycloak_client_rolescope",
	"keycloak_clientscope", "keycloak_clientscope_rolemappings",
	"keycloak_clientscope_type", "keycloak_clientsecret_info",
	"keycloak_clientsecret_regenerate", "keycloak_clienttemplate",
	"keycloak_component", "keycloak_component_info", "keycloak_group",
	"keycloak_identity_provider", "keycloak_realm", "keycloak_realm_info",
	"keycloak_realm_key", "keycloak_realm_keys_metadata_info",
	"keycloak_realm_localization", "keycloak_realm_rolemapping",
	"keycloak_realm_users_info", "keycloak_role", "keycloak_user",
	"keycloak_user_execute_actions_email", "keycloak_user_federation",
	"keycloak_user_rolemapping", "keycloak_userprofile", "keyring",
	"keyring_info", "kibana_plugin", "kopia_repository",
	"kopia_repository_info", "krb_ticket", "launchd", "layman", "lbu",
	"ldap_attrs", "ldap_entry", "ldap_inc", "ldap_passwd", "ldap_search",
	"linode", "linode_v4", "listen_ports_facts", "lldp_facts", "locale_gen",
	"logrotate", "logstash_plugin", "lvg", "lvg_rename", "lvm_pv",
	"lvm_pv_move_data", "lvol", "lxc_container", "lxd_container",
	"lxd_profile", "lxd_project", "lxd_storage_pool_info",
	"lxd_storage_volume_info", "macports", "mail", "make", "mas",
	"mattermost", "maven_artifact", "memset_dns_reload",
	"memset_memstore_info", "memset_server_info", "memset_zone",
	"memset_zone_domain", "memset_zone_record", "mksysb", "modprobe", "monit",
	"mqtt", "mssql_db", "mssql_script", "nagios", "newrelic_deployment",
	"nexmo", "nginx_status_info", "nictagadm", "nmcli", "nomad_job",
	"nomad_job_info", "nomad_token", "nosh", "npm", "nsupdate", "odbc",
	"ohai", "omapi_host", "one_host", "one_image", "one_image_info",
	"one_service", "one_template", "one_vm", "one_vnet", "onepassword_info",
	"open_iscsi", "openbsd_pkg", "opendj_backendprop", "openwrt_init", "opkg",
	"osx_defaults", "ovh_ip_failover", "ovh_ip_loadbalancing_backend",
	"ovh_monthly_billing", "pacemaker_cluster", "pacemaker_info",
	"pacemaker_resource", "pacemaker_stonith", "packet_device",
	"packet_ip_subnet", "packet_project", "packet_sshkey", "packet_volume",
	"packet_volume_attachment", "pacman", "pacman_key", "pam_limits", "pamd",
	"parted", "pear", "pids", "pip_package_info", "pipx", "pipx_info", "pkg5",
	"pkg5_publisher", "pkgin", "pkgng", "pkgutil", "pmem", "pnpm", "portage",
	"portinstall", "pritunl_org", "pritunl_org_info", "pritunl_user",
	"pritunl_user_info", "pulp_repo", "puppet", "pushover",
	"python_requirements_info", "read_csv", "redfish_command",
	"redfish_config", "redfish_info", "redhat_subscription", "redis",
	"redis_data", "redis_data_incr", "redis_data_info", "redis_info",
	"rhsm_release", "rhsm_repository", "riak", "rocketchat",
	"rollbar_deployment", "rpm_ostree_pkg", "rundeck_acl_policy",
	"rundeck_job_executions_info", "rundeck_job_run", "rundeck_project",
	"runit", "say", "scaleway_compute", "scaleway_compute_private_network",
	"scaleway_container", "scaleway_container_info",
	"scaleway_container_namespace", "scaleway_container_namespace_info",
	"scaleway_container_registry", "scaleway_container_registry_info",
	"scaleway_database_backup", "scaleway_function", "scaleway_function_info",
	"scaleway_function_namespace", "scaleway_function_namespace_info",
	"scaleway_image_info", "scaleway_ip", "scaleway_ip_info", "scaleway_lb",
	"scaleway_organization_info", "scaleway_private_network",
	"scaleway_security_group", "scaleway_security_group_info",
	"scaleway_security_group_rule", "scaleway_server_info",
	"scaleway_snapshot_info", "scaleway_sshkey", "scaleway_user_data",
	"scaleway_volume", "scaleway_volume_info", "sefcontext",
	"selinux_permissive", "selogin", "sendgrid", "seport", "serverless",
	"shutdown", "simpleinit_msb", "sl_vm", "slackpkg", "smartos_image_info",
	"snap", "snap_alias", "snap_connect", "snmp_facts", "solaris_zone",
	"sorcery", "spectrum_device", "spectrum_model_attrs", "ss_3par_cpg",
	"ssh_config", "sssd_info", "stacki_host", "statsd", "sudoers",
	"supervisorctl", "svc", "svr4pkg", "swdepot", "swupd", "syslogger",
	"syspatch", "sysrc", "systemd_creds_decrypt", "systemd_creds_encrypt",
	"systemd_info", "sysupgrade", "telegram", "terraform", "timezone",
	"twilio", "udm_dns_record", "udm_dns_zone", "udm_group", "udm_share",
	"udm_user", "ufw", "uptimerobot", "urpmi", "usb_facts", "uv_python",
	"vdo", "vertica_configuration", "vertica_info", "vertica_role",
	"vertica_schema", "vertica_user", "vmadm", "wakeonlan",
	"write_binary_file", "xattr", "xbps", "xcc_redfish_command", "xdg_mime",
	"xenserver_facts", "xenserver_guest", "xenserver_guest_info",
	"xenserver_guest_powerstate", "xfconf", "xfconf_info", "xfs_quota", "xml",
	"xml_info", "yarn", "yum_versionlock", "zfs", "zfs_delegate_admin",
	"zfs_facts", "znode", "zpool", "zpool_facts", "zypper",
	"zypper_repository", "zypper_repository_info",
}

var (
	collectionOnce sync.Once
	collectionOf   map[string]string
)

// Collection reports the collection a module name belongs to. A
// fully-qualified name resolves like any other, through NormalizeName.
func Collection(name string) string {
	collectionOnce.Do(func() {
		collectionOf = make(map[string]string, len(builtinModules)+len(posixModules)+len(communityGeneralModules))
		for _, n := range builtinModules {
			collectionOf[n] = "ansible.builtin"
		}
		for _, n := range posixModules {
			collectionOf[n] = "ansible.posix"
		}
		for _, n := range communityGeneralModules {
			collectionOf[n] = "community.general"
		}
	})
	return collectionOf[NormalizeName(name)]
}

// FQCN qualifies a module name the way real ansible-doc prints it:
// <collection>.<name>. A name already carrying a dot is returned
// unchanged, and one with no known collection falls back to
// ansible.builtin -- the old assumption, kept only as a last resort so
// an unlisted name still prints something shaped like a module name.
func FQCN(name string) string {
	if strings.Contains(name, ".") {
		return name
	}
	if c := Collection(name); c != "" {
		return c + "." + name
	}
	return "ansible.builtin." + name
}
