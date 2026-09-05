package modules

//go:generate go run ./internal/gendocs

// registerBuiltins registers this package's built-in module set onto r.
func registerBuiltins(r *Registry) {
	r.Register("command", moduleCommand)
	r.Register("shell", moduleShell)
	r.Register("copy", moduleCopy)
	r.Register("file", moduleFile)
	r.Register("template", moduleTemplate)
	r.Register("lineinfile", moduleLineinfile)
	r.Register("replace", moduleReplace)
	r.Register("stat", moduleStat)
	r.Register("debug", moduleDebug)
	r.Register("assert", moduleAssert)
	r.Register("fail", moduleFail)
	r.Register("set_fact", moduleSetFact)
	r.Register("cron", moduleCron)
	r.Register("user", moduleUser)
	r.Register("group", moduleGroup)
	r.Register("systemd", moduleSystemd)
	r.Register("service", moduleSystemd) // "service" is systemd's classic alias on modern distros
	r.Register("apt", moduleApt)
	r.Register("pip", modulePip)
	r.Register("git", moduleGit)
	r.Register("ping", modulePing)
	r.Register("slurp", moduleSlurp)
	r.Register("tempfile", moduleTempfile)
	r.Register("fetch", moduleFetch)
	r.Register("find", moduleFind)
	r.Register("get_url", moduleGetURL)
	r.Register("uri", moduleURI)
	r.Register("wait_for", moduleWaitFor)
	r.Register("hostname", moduleHostname)
	r.Register("getent", moduleGetent)
	r.Register("apt_key", moduleAptKey)
	r.Register("apt_repository", moduleAptRepository)
	r.Register("blockinfile", moduleBlockinfile)
	r.Register("debconf", moduleDebconf)
	r.Register("dnf", moduleDnf)
	r.Register("dnf5", moduleDnf5)
	r.Register("dpkg_selections", moduleDpkgSelections)
	r.Register("package", modulePackage)
	r.Register("package_facts", modulePackageFacts)
	r.Register("service_facts", moduleServiceFacts)

	// Batch 3: the remaining ansible.builtin modules/directives that
	// belong in this package (see the package-level module count note
	// in the project's PR history for the 9 engine-level directives —
	// add_host, group_by, import_playbook, import_role, import_tasks,
	// include_role, include_tasks, include_vars, meta — that are
	// deliberately NOT registered here, since they're handled directly
	// by go-ansible/playbook's engine, not by a module).
	r.Register("assemble", moduleAssemble)
	r.Register("async_status", moduleAsyncStatus)
	r.Register("deb822_repository", moduleDeb822Repository)
	r.Register("expect", moduleExpect)
	r.Register("gather_facts", moduleGatherFacts)
	r.Register("iptables", moduleIptables)
	r.Register("known_hosts", moduleKnownHosts)
	r.Register("mount_facts", moduleMountFacts)
	r.Register("pause", modulePause)
	r.Register("raw", moduleRaw)
	r.Register("reboot", moduleReboot)
	r.Register("rpm_key", moduleRpmKey)
	r.Register("script", moduleScript)
	r.Register("set_stats", moduleSetStats)
	r.Register("setup", moduleSetup)
	r.Register("subversion", moduleSubversion)
	r.Register("systemd_service", moduleSystemd) // "systemd_service" is systemd's canonical ansible-core name; "systemd"/"service" (registered above) are its own aliases
	r.Register("sysvinit", moduleSysvinit)
	r.Register("unarchive", moduleUnarchive)
	r.Register("validate_argument_spec", moduleValidateArgumentSpec)
	r.Register("wait_for_connection", moduleWaitForConnection)
	r.Register("yum_repository", moduleYumRepository)

	// ansible.posix collection (2.2.2): shell-command-based modules that
	// operate on a real machine, fitting this port's architecture the
	// same way ansible.builtin's own modules do. Cloud-provider
	// collections are out of scope (they need real API client SDKs, not
	// shell composition over a Connection) — see this batch's PR
	// description for the full rationale.
	r.Register("acl", moduleAcl)
	r.Register("at", moduleAt)
	r.Register("authorized_key", moduleAuthorizedKey)
	r.Register("firewalld", moduleFirewalld)
	r.Register("firewalld_info", moduleFirewalldInfo)
	r.Register("mount", moduleMount)
	r.Register("patch", modulePatch)
	r.Register("rhel_facts", moduleRhelFacts)
	r.Register("rhel_rpm_ostree", moduleRhelRpmOstree)
	r.Register("rpm_ostree_upgrade", moduleRpmOstreeUpgrade)
	r.Register("seboolean", moduleSeboolean)
	r.Register("selinux", moduleSelinux)
	// synchronize is registered as an honest, always-failing stub — see
	// synchronize.go's doc comment for why a partial implementation was
	// rejected (the same "fail loud, not silently wrong" convention
	// async_status.go and pause.go's no-duration form already use).
	r.Register("synchronize", moduleSynchronize)
	r.Register("sysctl", moduleSysctl)

	// community.general collection (batch 1, 50 of 577 modules): a
	// curated first slice deliberately excluding SaaS-API wrappers
	// (Slack/PagerDuty/GitLab/Jenkins/etc.) and cloud-VPS providers
	// (Scaleway/Linode/OpenNebula/etc.), which need a fundamentally
	// different architecture (real API client SDKs, not shell
	// composition over a Connection) — same rationale as the
	// ansible.posix batch's own cloud-collection exclusion above.

	// Package/dependency managers.
	r.Register("apk", moduleApk)
	r.Register("homebrew", moduleHomebrew)
	r.Register("homebrew_cask", moduleHomebrewCask)
	r.Register("homebrew_tap", moduleHomebrewTap)
	r.Register("homebrew_services", moduleHomebrewServices)
	r.Register("snap", moduleSnap)
	r.Register("snap_alias", moduleSnapAlias)
	r.Register("flatpak", moduleFlatpak)
	r.Register("flatpak_remote", moduleFlatpakRemote)
	r.Register("pacman", modulePacman)
	r.Register("pacman_key", modulePacmanKey)
	r.Register("npm", moduleNpm)
	r.Register("yarn", moduleYarn)
	r.Register("pnpm", modulePnpm)
	r.Register("gem", moduleGem)
	r.Register("bundler", moduleBundler)
	r.Register("composer", moduleComposer)

	// Language/dev tooling.
	r.Register("cpanm", moduleCpanm)
	r.Register("cargo", moduleCargo)
	r.Register("golang_package", moduleGolangPackage)
	r.Register("maven_artifact", moduleMavenArtifact)
	r.Register("pear", modulePear)
	r.Register("opkg", moduleOpkg)
	r.Register("django_command", moduleDjangoCommand)
	r.Register("django_manage", moduleDjangoManage)

	// Filesystem/data.
	r.Register("archive", moduleArchive)
	r.Register("decompress", moduleDecompress)
	r.Register("filesize", moduleFilesize)
	r.Register("ini_file", moduleIniFile)
	r.Register("xml", moduleXml)
	r.Register("read_csv", moduleReadCsv)
	r.Register("alternatives", moduleAlternatives)
	r.Register("capabilities", moduleCapabilities)
	r.Register("xattr", moduleXattr)
	r.Register("crypttab", moduleCrypttab)

	// System config.
	r.Register("sudoers", moduleSudoers)
	r.Register("pam_limits", modulePamLimits)
	r.Register("pamd", modulePamd)
	r.Register("timezone", moduleTimezone)
	r.Register("locale_gen", moduleLocaleGen)
	r.Register("modprobe", moduleModprobe)
	r.Register("kernel_blacklist", moduleKernelBlacklist)
	r.Register("cronvar", moduleCronvar)
	r.Register("logrotate", moduleLogrotate)
	r.Register("supervisorctl", moduleSupervisorctl)

	// Misc.
	r.Register("git_config", moduleGitConfig)
	r.Register("ssh_config", moduleSSHConfig)
	r.Register("htpasswd", moduleHtpasswd)
	r.Register("java_cert", moduleJavaCert)
	r.Register("mail", moduleMail)

	// community.general collection (batch 2, 50 more of 577 modules): a
	// second curated slice, same exclusion rationale as batch 1 above
	// (no SaaS-API wrappers, no cloud-VPS providers).

	// Package managers (more distros).
	r.Register("zypper", moduleZypper)
	r.Register("zypper_repository", moduleZypperRepository)
	r.Register("zypper_repository_info", moduleZypperRepositoryInfo)
	r.Register("dnf_versionlock", moduleDnfVersionlock)
	r.Register("yum_versionlock", moduleYumVersionlock)
	r.Register("dnf_config_manager", moduleDnfConfigManager)
	r.Register("macports", moduleMacports)
	r.Register("pkgin", modulePkgin)
	r.Register("pkgng", modulePkgng)
	r.Register("xbps", moduleXbps)
	r.Register("slackpkg", moduleSlackpkg)
	r.Register("openbsd_pkg", moduleOpenbsdPkg)

	// Filesystem/storage.
	r.Register("filesystem", moduleFilesystem)
	r.Register("parted", moduleParted)
	r.Register("lvg", moduleLvg)
	r.Register("lvg_rename", moduleLvgRename)
	r.Register("lvol", moduleLvol)
	r.Register("lvm_pv", moduleLvmPv)
	r.Register("lvm_pv_move_data", moduleLvmPvMoveData)
	r.Register("btrfs_subvolume", moduleBtrfsSubvolume)
	r.Register("btrfs_info", moduleBtrfsInfo)
	r.Register("zfs", moduleZfs)
	r.Register("zpool", moduleZpool)
	r.Register("vdo", moduleVdo)

	// Networking.
	r.Register("nmcli", moduleNmcli)
	r.Register("interfaces_file", moduleInterfacesFile)
	r.Register("iptables_state", moduleIptablesState)
	r.Register("ufw", moduleUfw)
	r.Register("nsupdate", moduleNsupdate)
	r.Register("wakeonlan", moduleWakeonlan)

	// System/service management.
	r.Register("puppet", modulePuppet)
	r.Register("monit", moduleMonit)
	r.Register("runit", moduleRunit)
	r.Register("sysrc", moduleSysrc)
	r.Register("syspatch", moduleSyspatch)
	r.Register("homectl", moduleHomectl)
	r.Register("systemd_info", moduleSystemdInfo)
	r.Register("listen_ports_facts", moduleListenPortsFacts)
	r.Register("usb_facts", moduleUsbFacts)

	// SELinux.
	r.Register("selinux_permissive", moduleSelinuxPermissive)
	r.Register("sefcontext", moduleSefcontext)
	r.Register("selogin", moduleSelogin)
	r.Register("seport", moduleSeport)

	// Read-only facts.
	r.Register("xml_info", moduleXmlInfo)
	r.Register("git_config_info", moduleGitConfigInfo)
	r.Register("pip_package_info", modulePipPackageInfo)
	r.Register("python_requirements_info", modulePythonRequirementsInfo)

	// Cluster (Pacemaker).
	r.Register("pacemaker_cluster", modulePacemakerCluster)
	r.Register("pacemaker_resource", modulePacemakerResource)
	r.Register("pacemaker_info", modulePacemakerInfo)

	// community.general collection (batch 3, 50 more of 577 modules): a
	// third curated slice, same exclusion rationale as batches 1-2
	// above (no SaaS-API wrappers, no cloud-VPS/hardware providers).

	// Storage/ZFS facts.
	r.Register("zfs_facts", moduleZfsFacts)
	r.Register("zpool_facts", moduleZpoolFacts)
	r.Register("zfs_delegate_admin", moduleZfsDelegateAdmin)
	r.Register("xfs_quota", moduleXfsQuota)

	// More package managers.
	r.Register("pkg5", modulePkg5)
	r.Register("pkg5_publisher", modulePkg5Publisher)
	r.Register("pkgutil", modulePkgutil)
	r.Register("portage", modulePortage)
	r.Register("portinstall", modulePortinstall)
	r.Register("installp", moduleInstallp)
	r.Register("svr4pkg", moduleSvr4pkg)
	r.Register("pipx", modulePipx)
	r.Register("pipx_info", modulePipxInfo)

	// LDAP.
	r.Register("ldap_attrs", moduleLdapAttrs)
	r.Register("ldap_entry", moduleLdapEntry)
	r.Register("ldap_passwd", moduleLdapPasswd)
	r.Register("ldap_search", moduleLdapSearch)

	// FreeIPA.
	r.Register("ipa_user", moduleIpaUser)
	r.Register("ipa_group", moduleIpaGroup)
	r.Register("ipa_host", moduleIpaHost)
	r.Register("ipa_hostgroup", moduleIpaHostgroup)
	r.Register("ipa_dnsrecord", moduleIpaDnsrecord)
	r.Register("ipa_dnszone", moduleIpaDnszone)
	r.Register("ipa_sudorule", moduleIpaSudorule)
	r.Register("ipa_hbacrule", moduleIpaHbacrule)
	r.Register("ipa_role", moduleIpaRole)
	r.Register("ipa_service", moduleIpaService)

	// Redis.
	r.Register("redis", moduleRedis)
	r.Register("redis_info", moduleRedisInfo)
	r.Register("redis_data", moduleRedisData)
	r.Register("redis_data_info", moduleRedisDataInfo)
	r.Register("redis_data_incr", moduleRedisDataIncr)

	// Consul KV.
	r.Register("consul_kv", moduleConsulKv)
	r.Register("consul_kv_info", moduleConsulKvInfo)

	// Kerberos.
	r.Register("krb_ticket", moduleKrbTicket)

	// Misc admin.
	r.Register("open_iscsi", moduleOpenIscsi)
	r.Register("nagios", moduleNagios)
	r.Register("deploy_helper", moduleDeployHelper)
	r.Register("haproxy", moduleHaproxy)
	r.Register("terraform", moduleTerraform)

	// Desktop/system config.
	r.Register("dconf", moduleDconf)
	r.Register("gio_mime", moduleGioMime)
	r.Register("xdg_mime", moduleXdgMime)
	r.Register("osx_defaults", moduleOsxDefaults)
	r.Register("launchd", moduleLaunchd)
	r.Register("kdeconfig", moduleKdeconfig)
	r.Register("java_keystore", moduleJavaKeystore)

	// More read-only facts.
	r.Register("snmp_facts", moduleSNMPFacts)
	r.Register("lldp_facts", moduleLLDPFacts)
	r.Register("pids", modulePids)

	// community.general collection (batch 4, 50 more of 577 modules): a
	// fourth curated slice, same exclusion rationale as batches 1-3
	// above (no SaaS-API wrappers, no cloud-VPS/hardware providers).

	// More FreeIPA.
	r.Register("ipa_config", moduleIpaConfig)
	r.Register("ipa_getkeytab", moduleIpaGetkeytab)
	r.Register("ipa_otpconfig", moduleIpaOtpconfig)
	r.Register("ipa_otptoken", moduleIpaOtptoken)
	r.Register("ipa_pwpolicy", moduleIpaPwpolicy)
	r.Register("ipa_subca", moduleIpaSubca)
	r.Register("ipa_sudocmd", moduleIpaSudocmd)
	r.Register("ipa_sudocmdgroup", moduleIpaSudocmdgroup)
	r.Register("ipa_vault", moduleIpaVault)

	// More facts.
	r.Register("facter_facts", moduleFacterFacts)
	r.Register("ohai", moduleOhai)
	r.Register("cloud_init_data_facts", moduleCloudInitDataFacts)
	r.Register("sssd_info", moduleSssdInfo)
	r.Register("nginx_status_info", moduleNginxStatusInfo)

	// More Consul.
	r.Register("consul", moduleConsul)
	r.Register("consul_agent_check", moduleConsulAgentCheck)
	r.Register("consul_agent_service", moduleConsulAgentService)
	r.Register("consul_acl_bootstrap", moduleConsulACLBootstrap)
	r.Register("consul_auth_method", moduleConsulAuthMethod)
	r.Register("consul_binding_rule", moduleConsulBindingRule)
	r.Register("consul_policy", moduleConsulPolicy)
	r.Register("consul_role", moduleConsulRole)
	r.Register("consul_session", moduleConsulSession)
	r.Register("consul_token", moduleConsulToken)

	// More desktop/system config.
	r.Register("systemd_creds_decrypt", moduleSystemdCredsDecrypt)
	r.Register("systemd_creds_encrypt", moduleSystemdCredsEncrypt)
	r.Register("xfconf", moduleXfconf)
	r.Register("xfconf_info", moduleXfconfInfo)

	// LXD/LXC containers.
	r.Register("lxd_container", moduleLxdContainer)
	r.Register("lxd_profile", moduleLxdProfile)
	r.Register("lxd_project", moduleLxdProject)
	r.Register("lxd_storage_pool_info", moduleLxdStoragePoolInfo)
	r.Register("lxd_storage_volume_info", moduleLxdStorageVolumeInfo)
	r.Register("lxc_container", moduleLxcContainer)

	// HashiCorp Nomad.
	r.Register("nomad_job", moduleNomadJob)
	r.Register("nomad_job_info", moduleNomadJobInfo)
	r.Register("nomad_token", moduleNomadToken)

	// Misc.
	r.Register("znode", moduleZnode)
	r.Register("uv_python", moduleUvPython)
	r.Register("mas", moduleMas)

	// Database admin.
	r.Register("mssql_db", moduleMssqlDb)
	r.Register("mssql_script", moduleMssqlScript)
	r.Register("vertica_configuration", moduleVerticaConfiguration)
	r.Register("vertica_info", moduleVerticaInfo)
	r.Register("vertica_role", moduleVerticaRole)
	r.Register("vertica_schema", moduleVerticaSchema)
	r.Register("vertica_user", moduleVerticaUser)

	// RHEL subscription management.
	r.Register("redhat_subscription", moduleRedhatSubscription)
	r.Register("rhsm_release", moduleRhsmRelease)
	r.Register("rhsm_repository", moduleRhsmRepository)

	// community.general collection (batch 5, 50 more of 577 modules): a
	// fifth curated slice, same exclusion rationale as batches 1-4 above
	// (no SaaS-API wrappers, no cloud-VPS/hardware providers). By this
	// batch the remaining pool skewed heavily toward excluded categories
	// (keycloak/gitlab/jenkins/github/scaleway/hwc/oneview/utm/redfish
	// families), so curation required more deliberate searching.

	// AIX.
	r.Register("aix_devices", moduleAixDevices)
	r.Register("aix_filesystem", moduleAixFilesystem)
	r.Register("aix_inittab", moduleAixInittab)
	r.Register("aix_lvg", moduleAixLvg)
	r.Register("aix_lvol", moduleAixLvol)

	// More OS-specific system tools.
	r.Register("beadm", moduleBeadm)
	r.Register("bootc_manage", moduleBootcManage)
	r.Register("mksysb", moduleMksysb)
	r.Register("rpm_ostree_pkg", moduleRpmOstreePkg)
	r.Register("openwrt_init", moduleOpenwrtInit)
	r.Register("awall", moduleAwall)
	r.Register("ip_netns", moduleIPNetns)
	r.Register("dpkg_divert", moduleDpkgDivert)

	// Elastic Stack plugin managers.
	r.Register("elasticsearch_plugin", moduleElasticsearchPlugin)
	r.Register("logstash_plugin", moduleLogstashPlugin)
	r.Register("kibana_plugin", moduleKibanaPlugin)

	// InfluxDB.
	r.Register("influxdb_database", moduleInfluxdbDatabase)
	r.Register("influxdb_query", moduleInfluxdbQuery)
	r.Register("influxdb_retention_policy", moduleInfluxdbRetentionPolicy)
	r.Register("influxdb_user", moduleInfluxdbUser)
	r.Register("influxdb_write", moduleInfluxdbWrite)

	// Icinga2.
	r.Register("icinga2_feature", moduleIcinga2Feature)
	r.Register("icinga2_host", moduleIcinga2Host)
	r.Register("icinga2_downtime", moduleIcinga2Downtime)

	// Kopia backup.
	r.Register("kopia_repository", moduleKopiaRepository)
	r.Register("kopia_repository_info", moduleKopiaRepositoryInfo)

	// Version control.
	r.Register("bzr", moduleBzr)
	r.Register("hg", moduleHg)

	// Web/app servers.
	r.Register("apache2_module", moduleApache2Module)
	r.Register("apache2_mod_proxy", moduleApache2ModProxy)
	r.Register("jboss", moduleJboss)

	// LDAP/directory.
	r.Register("ldap_inc", moduleLdapInc)
	r.Register("opendj_backendprop", moduleOpendjBackendprop)

	// ISO tools.
	r.Register("iso_create", moduleIsoCreate)
	r.Register("iso_customize", moduleIsoCustomize)
	r.Register("iso_extract", moduleIsoExtract)

	// Desktop config.
	r.Register("gconftool2", moduleGconftool2)
	r.Register("gconftool2_info", moduleGconftool2Info)

	// Misc admin.
	r.Register("android_sdk", moduleAndroidSdk)
	r.Register("copr", moduleCopr)
	r.Register("etcd3", moduleEtcd3)
	r.Register("gunicorn", moduleGunicorn)
	r.Register("layman", moduleLayman)
	r.Register("lbu", moduleLbu)
	r.Register("make", moduleMake)
	r.Register("mqtt", moduleMqtt)
	r.Register("omapi_host", moduleOmapiHost)
	r.Register("pmem", modulePmem)
	r.Register("shutdown", moduleShutdown)
	r.Register("ejabberd_user", moduleEjabberdUser)

	// community.general collection (batch 6, 35 more of 577 modules): a
	// sixth, deliberately SMALLER curated slice — by this batch the
	// remaining pool is dominated by SaaS/cloud/hardware-vendor modules
	// this port excludes (keycloak/gitlab/jenkins/github/scaleway/hwc/
	// oneview/utm/redfish/ibm_sa families), so the genuinely portable
	// remainder no longer supports a full 50-module batch.

	// Provisioning + Django extensions + IPMI.
	r.Register("cobbler_sync", moduleCobblerSync)
	r.Register("cobbler_system", moduleCobblerSystem)
	r.Register("django_check", moduleDjangoCheck)
	r.Register("django_createcachetable", moduleDjangoCreateCacheTable)
	r.Register("django_dumpdata", moduleDjangoDumpData)
	r.Register("django_loaddata", moduleDjangoLoadData)
	r.Register("ipmi_boot", moduleIPMIBoot)
	r.Register("ipmi_power", moduleIPMIPower)
	r.Register("apt_repo", moduleAptRepo)
	r.Register("apt_rpm", moduleAptRpm)
	r.Register("snap_connect", moduleSnapConnect)
	r.Register("solaris_zone", moduleSolarisZone)

	// Niche package managers + process supervision + misc.
	r.Register("sorcery", moduleSorcery)
	r.Register("statsd", moduleStatsd)
	r.Register("svc", moduleSvc)
	r.Register("swdepot", moduleSwdepot)
	r.Register("swupd", moduleSwupd)
	r.Register("syslogger", moduleSyslogger)
	r.Register("sysupgrade", moduleSysupgrade)
	r.Register("urpmi", moduleUrpmi)
	r.Register("write_binary_file", moduleWriteBinaryFile)
	r.Register("imgadm", moduleImgadm)
	r.Register("smartos_image_info", moduleSmartosImageInfo)
	r.Register("nictagadm", moduleNictagadm)

	// Univention UDM.
	r.Register("udm_dns_record", moduleUdmDnsRecord)
	r.Register("udm_dns_zone", moduleUdmDnsZone)
	r.Register("udm_group", moduleUdmGroup)
	r.Register("udm_share", moduleUdmShare)
	r.Register("udm_user", moduleUdmUser)

	// XenServer.
	r.Register("xenserver_facts", moduleXenserverFacts)
	r.Register("xenserver_guest", moduleXenserverGuest)
	r.Register("xenserver_guest_info", moduleXenserverGuestInfo)
	r.Register("xenserver_guest_powerstate", moduleXenserverGuestPowerstate)

	// Misc init systems.
	r.Register("nosh", moduleNosh)
	r.Register("simpleinit_msb", moduleSimpleinitMsb)

	// community.general collection (batch 7, 37 more of 577 modules): a
	// seventh curated slice. By this batch the exclusion doctrine used
	// since batch 1 (no SaaS-API wrappers) was deliberately narrowed:
	// github_*/gitlab_* modules were reconsidered and INCLUDED, since
	// both platforms ship official CLIs (gh/glab) this port can shell
	// out to, extending the same CLI-substitution precedent already
	// used for Consul/Redis/Terraform/Icinga2/Kopia — unlike Slack/
	// PagerDuty/Jenkins/Scaleway/Keycloak/etc., which have no comparable
	// universal official CLI and remain excluded.

	// GitHub, via the `gh` CLI.
	r.Register("github_deploy_key", moduleGithubDeployKey)
	r.Register("github_issue", moduleGithubIssue)
	r.Register("github_key", moduleGithubKey)
	r.Register("github_release", moduleGithubRelease)
	r.Register("github_repo", moduleGithubRepo)
	r.Register("github_secrets", moduleGithubSecrets)
	r.Register("github_secrets_info", moduleGithubSecretsInfo)
	r.Register("github_webhook", moduleGithubWebhook)
	r.Register("github_webhook_info", moduleGithubWebhookInfo)

	// GitLab, via the `glab` CLI.
	r.Register("gitlab_branch", moduleGitlabBranch)
	r.Register("gitlab_deploy_key", moduleGitlabDeployKey)
	r.Register("gitlab_group", moduleGitlabGroup)
	r.Register("gitlab_group_access_token", moduleGitlabGroupAccessToken)
	r.Register("gitlab_group_members", moduleGitlabGroupMembers)
	r.Register("gitlab_group_variable", moduleGitlabGroupVariable)
	r.Register("gitlab_hook", moduleGitlabHook)
	r.Register("gitlab_instance_variable", moduleGitlabInstanceVariable)
	r.Register("gitlab_issue", moduleGitlabIssue)
	r.Register("gitlab_label", moduleGitlabLabel)
	r.Register("gitlab_merge_request", moduleGitlabMergeRequest)
	r.Register("gitlab_milestone", moduleGitlabMilestone)
	r.Register("gitlab_project", moduleGitlabProject)
	r.Register("gitlab_project_access_token", moduleGitlabProjectAccessToken)
	r.Register("gitlab_project_approvals", moduleGitlabProjectApprovals)
	r.Register("gitlab_project_badge", moduleGitlabProjectBadge)
	r.Register("gitlab_project_members", moduleGitlabProjectMembers)
	r.Register("gitlab_project_variable", moduleGitlabProjectVariable)
	r.Register("gitlab_protected_branch", moduleGitlabProtectedBranch)
	r.Register("gitlab_runner", moduleGitlabRunner)
	r.Register("gitlab_user", moduleGitlabUser)

	// Misc portable tools.
	r.Register("keyring", moduleKeyring)
	r.Register("keyring_info", moduleKeyringInfo)
	r.Register("odbc", moduleOdbc)
	r.Register("riak", moduleRiak)
	r.Register("say", moduleSay)
	r.Register("serverless", moduleServerless)
	r.Register("vmadm", moduleVmadm)

	// community.general collection (batch 8, 89 more of 577 modules): an
	// eighth curated slice, a further, deliberate narrowing of the
	// exclusion doctrine — Keycloak, Scaleway, Huawei Cloud, Jenkins,
	// Equinix Metal, and Linode were reconsidered and INCLUDED because
	// each ships a genuine official CLI (kcadm.sh/scw/KooCLI("hcloud")/
	// jenkins-cli.jar/metal/linode-cli respectively), extending the same
	// CLI-substitution precedent already used for Consul/Redis/
	// Terraform/Icinga2/Kopia/GitHub(gh)/GitLab(glab). This IS still a
	// meaningfully bigger doctrine shift than GitHub/GitLab was — three
	// of these (Scaleway/Huawei Cloud/Equinix Metal) are cloud-VPS
	// providers, a category this port's own docs named as an exclusion
	// EXAMPLE since batch 1 — confirmed explicitly with the user before
	// dispatch, not decided unilaterally.

	// Keycloak, via kcadm.sh.
	r.Register("keycloak_authentication", moduleKeycloakAuthentication)
	r.Register("keycloak_authentication_required_actions", moduleKeycloakAuthenticationRequiredActions)
	r.Register("keycloak_authentication_v2", moduleKeycloakAuthenticationV2)
	r.Register("keycloak_authz_authorization_scope", moduleKeycloakAuthzAuthorizationScope)
	r.Register("keycloak_authz_custom_policy", moduleKeycloakAuthzCustomPolicy)
	r.Register("keycloak_authz_permission", moduleKeycloakAuthzPermission)
	r.Register("keycloak_authz_permission_info", moduleKeycloakAuthzPermissionInfo)
	r.Register("keycloak_client", moduleKeycloakClient)
	r.Register("keycloak_client_rolemapping", moduleKeycloakClientRolemapping)
	r.Register("keycloak_client_rolescope", moduleKeycloakClientRolescope)
	r.Register("keycloak_clientscope", moduleKeycloakClientscope)
	r.Register("keycloak_clientscope_rolemappings", moduleKeycloakClientscopeRolemappings)
	r.Register("keycloak_clientscope_type", moduleKeycloakClientscopeType)
	r.Register("keycloak_clientsecret_info", moduleKeycloakClientsecretInfo)
	r.Register("keycloak_clientsecret_regenerate", moduleKeycloakClientsecretRegenerate)
	r.Register("keycloak_clienttemplate", moduleKeycloakClienttemplate)
	r.Register("keycloak_component", moduleKeycloakComponent)
	r.Register("keycloak_component_info", moduleKeycloakComponentInfo)
	r.Register("keycloak_group", moduleKeycloakGroup)
	r.Register("keycloak_identity_provider", moduleKeycloakIdentityProvider)
	r.Register("keycloak_realm", moduleKeycloakRealm)
	r.Register("keycloak_realm_info", moduleKeycloakRealmInfo)
	r.Register("keycloak_realm_key", moduleKeycloakRealmKey)
	r.Register("keycloak_realm_keys_metadata_info", moduleKeycloakRealmKeysMetadataInfo)
	r.Register("keycloak_realm_localization", moduleKeycloakRealmLocalization)
	r.Register("keycloak_realm_rolemapping", moduleKeycloakRealmRolemapping)
	r.Register("keycloak_realm_users_info", moduleKeycloakRealmUsersInfo)
	r.Register("keycloak_role", moduleKeycloakRole)
	r.Register("keycloak_user", moduleKeycloakUser)
	r.Register("keycloak_user_execute_actions_email", moduleKeycloakUserExecuteActionsEmail)
	r.Register("keycloak_user_federation", moduleKeycloakUserFederation)
	r.Register("keycloak_user_rolemapping", moduleKeycloakUserRolemapping)
	r.Register("keycloak_userprofile", moduleKeycloakUserprofile)

	// Scaleway, via scw.
	r.Register("scaleway_compute", moduleScalewayCompute)
	r.Register("scaleway_compute_private_network", moduleScalewayComputePrivateNetwork)
	r.Register("scaleway_container", moduleScalewayContainer)
	r.Register("scaleway_container_info", moduleScalewayContainerInfo)
	r.Register("scaleway_container_namespace", moduleScalewayContainerNamespace)
	r.Register("scaleway_container_namespace_info", moduleScalewayContainerNamespaceInfo)
	r.Register("scaleway_container_registry", moduleScalewayContainerRegistry)
	r.Register("scaleway_container_registry_info", moduleScalewayContainerRegistryInfo)
	r.Register("scaleway_database_backup", moduleScalewayDatabaseBackup)
	r.Register("scaleway_function", moduleScalewayFunction)
	r.Register("scaleway_function_info", moduleScalewayFunctionInfo)
	r.Register("scaleway_function_namespace", moduleScalewayFunctionNamespace)
	r.Register("scaleway_function_namespace_info", moduleScalewayFunctionNamespaceInfo)
	r.Register("scaleway_image_info", moduleScalewayImageInfo)
	r.Register("scaleway_ip", moduleScalewayIP)
	r.Register("scaleway_ip_info", moduleScalewayIPInfo)
	r.Register("scaleway_lb", moduleScalewayLB)
	r.Register("scaleway_organization_info", moduleScalewayOrganizationInfo)
	r.Register("scaleway_private_network", moduleScalewayPrivateNetwork)
	r.Register("scaleway_security_group", moduleScalewaySecurityGroup)
	r.Register("scaleway_security_group_info", moduleScalewaySecurityGroupInfo)
	r.Register("scaleway_security_group_rule", moduleScalewaySecurityGroupRule)
	r.Register("scaleway_server_info", moduleScalewayServerInfo)
	r.Register("scaleway_snapshot_info", moduleScalewaySnapshotInfo)
	r.Register("scaleway_sshkey", moduleScalewaySSHKey)
	r.Register("scaleway_user_data", moduleScalewayUserData)
	r.Register("scaleway_volume", moduleScalewayVolume)
	r.Register("scaleway_volume_info", moduleScalewayVolumeInfo)

	// Huawei Cloud, via KooCLI ("hcloud" binary).
	r.Register("hwc_ecs_instance", moduleHwcEcsInstance)
	r.Register("hwc_evs_disk", moduleHwcEvsDisk)
	r.Register("hwc_network_vpc", moduleHwcNetworkVpc)
	r.Register("hwc_smn_topic", moduleHwcSmnTopic)
	r.Register("hwc_vpc_eip", moduleHwcVpcEip)
	r.Register("hwc_vpc_peering_connect", moduleHwcVpcPeeringConnect)
	r.Register("hwc_vpc_port", moduleHwcVpcPort)
	r.Register("hwc_vpc_private_ip", moduleHwcVpcPrivateIp)
	r.Register("hwc_vpc_route", moduleHwcVpcRoute)
	r.Register("hwc_vpc_security_group", moduleHwcVpcSecurityGroup)
	r.Register("hwc_vpc_security_group_rule", moduleHwcVpcSecurityGroupRule)
	r.Register("hwc_vpc_subnet", moduleHwcVpcSubnet)

	// Jenkins, via jenkins-cli.jar.
	r.Register("jenkins_build", moduleJenkinsBuild)
	r.Register("jenkins_build_info", moduleJenkinsBuildInfo)
	r.Register("jenkins_credential", moduleJenkinsCredential)
	r.Register("jenkins_job", moduleJenkinsJob)
	r.Register("jenkins_job_info", moduleJenkinsJobInfo)
	r.Register("jenkins_node", moduleJenkinsNode)
	r.Register("jenkins_plugin", moduleJenkinsPlugin)
	r.Register("jenkins_script", moduleJenkinsScript)

	// Equinix Metal / Packet, via the metal CLI.
	r.Register("packet_device", modulePacketDevice)
	r.Register("packet_ip_subnet", modulePacketIpSubnet)
	r.Register("packet_project", modulePacketProject)
	r.Register("packet_sshkey", modulePacketSshkey)
	r.Register("packet_volume", modulePacketVolume)
	r.Register("packet_volume_attachment", modulePacketVolumeAttachment)

	// Linode, via linode-cli.
	r.Register("linode", moduleLinode)
	r.Register("linode_v4", moduleLinodeV4)

	// community.general collection (batch 9, 26 more of 577 modules): a
	// ninth curated slice, applying the same already-confirmed
	// CLI-substitution principle (batches 7-8) to a final sweep of the
	// remaining pool. One platform, Pritunl, was investigated and found
	// to have NO usable official CLI for org/user management (its
	// `pritunl` CLI is server-lifecycle-only) — those 4 modules are
	// registered but fail loud rather than fake parity, the same
	// honest-gap convention packet_volume.go established in batch 8.

	// OpenNebula, via its per-resource CLIs (onehost/oneimage/
	// onetemplate/onevm/onevnet/oneflow).
	r.Register("one_host", moduleOneHost)
	r.Register("one_image", moduleOneImage)
	r.Register("one_image_info", moduleOneImageInfo)
	r.Register("one_service", moduleOneService)
	r.Register("one_template", moduleOneTemplate)
	r.Register("one_vm", moduleOneVM)
	r.Register("one_vnet", moduleOneVnet)

	// Rundeck, via the rd CLI.
	r.Register("rundeck_acl_policy", moduleRundeckACLPolicy)
	r.Register("rundeck_job_executions_info", moduleRundeckJobExecutionsInfo)
	r.Register("rundeck_job_run", moduleRundeckJobRun)
	r.Register("rundeck_project", moduleRundeckProject)

	// More Pacemaker, via pcs.
	r.Register("pacemaker_stonith", modulePacemakerStonith)

	// Stacki, via the stack CLI.
	r.Register("stacki_host", moduleStackiHost)

	// Pritunl: investigated, no usable official CLI for org/user
	// management — these fail loud rather than fake parity.
	r.Register("pritunl_org", modulePritunlOrg)
	r.Register("pritunl_org_info", modulePritunlOrgInfo)
	r.Register("pritunl_user", modulePritunlUser)
	r.Register("pritunl_user_info", modulePritunlUserInfo)

	// Alibaba Cloud, via aliyun (aliyun-cli).
	r.Register("ali_instance", moduleAliInstance)
	r.Register("ali_instance_info", moduleAliInstanceInfo)

	// IBM SoftLayer, via slcli.
	r.Register("sl_vm", moduleSlVm)

	// 1Password, via the op CLI.
	r.Register("onepassword_info", moduleOnepasswordInfo)

	// Pulp, via pulp-cli.
	r.Register("pulp_repo", modulePulpRepo)

	// Twilio, via twilio-cli.
	r.Register("twilio", moduleTwilio)

	// Aerospike, via asadm.
	r.Register("aerospike_migrations", moduleAerospikeMigrations)

	// Alerta, via the alerta CLI.
	r.Register("alerta_customer", moduleAlertaCustomer)

	// DNS, via ipwcli (already CLI-driven in the real module).
	r.Register("ipwcli_dns", moduleIpwcliDns)

	// community.general collection (batch 10, 29 more of 577 modules): a
	// tenth curated slice. Two modules (bower, easy_install) are trivial,
	// doctrine-free ports — the real modules already shell out to those
	// exact local CLIs directly, same as npm/yarn/pnpm/pip. file_remove
	// is a pure local-filesystem operation, no CLI question at all. The
	// remainder extends the already-confirmed CLI-substitution principle
	// to a further sweep of newly-verified official CLIs (Heroku,
	// Mattermost, New Relic, DNSimple, ipinfo.io, Cloudflare, OVHcloud,
	// Dell EMC VNX, IBM Spectrum Accelerate, HPE 3PAR), plus a genuine
	// third doctrine-widening decision, explicitly confirmed with the
	// user: the Redfish hardware-vendor family (idrac_redfish_*,
	// ilo_redfish_*, xcc_redfish_command), previously named as an
	// exclusion example since batch 5 — reconsidered because HPE's
	// `ilorest` and Lenovo's `OneCli` are genuine, strong-fit official
	// Redfish CLIs, and Dell's `racadm` is a real official (if older,
	// parallel) interface covering overlapping ground; Western Digital
	// was investigated and found to have no official CLI, so
	// wdc_redfish_* and the vendor-neutral redfish_command/config/info
	// remain excluded. hponcfg is not a substitution at all — the real
	// module already shells out to that exact HP-published local binary.

	// Trivial, doctrine-free (real modules already shell out locally).
	r.Register("bower", moduleBower)
	r.Register("easy_install", moduleEasyInstall)
	r.Register("file_remove", moduleFileRemove)

	// Heroku, via the heroku CLI.
	r.Register("heroku_collaborator", moduleHerokuCollaborator)

	// Mattermost, via mmctl.
	r.Register("mattermost", moduleMattermost)

	// New Relic, via the newrelic CLI.
	r.Register("newrelic_deployment", moduleNewrelicDeployment)

	// DNSimple, via dnsimple-cli.
	r.Register("dnsimple", moduleDnsimple)
	r.Register("dnsimple_info", moduleDnsimpleInfo)

	// ipinfo.io, via the ipinfo CLI.
	r.Register("ipinfoio_facts", moduleIpinfoioFacts)

	// Cloudflare, via flarectl.
	r.Register("cloudflare_dns", moduleCloudflareDns)

	// OVHcloud, via ovhcloud-cli.
	r.Register("ovh_ip_failover", moduleOvhIPFailover)
	r.Register("ovh_ip_loadbalancing_backend", moduleOvhIPLoadbalancingBackend)
	r.Register("ovh_monthly_billing", moduleOvhMonthlyBilling)

	// Dell EMC VNX, via naviseccli.
	r.Register("emc_vnx_sg_member", moduleEmcVnxSgMember)

	// IBM Spectrum Accelerate / XIV, via xcli.
	r.Register("ibm_sa_domain", moduleIbmSaDomain)
	r.Register("ibm_sa_host", moduleIbmSaHost)
	r.Register("ibm_sa_host_ports", moduleIbmSaHostPorts)
	r.Register("ibm_sa_pool", moduleIbmSaPool)
	r.Register("ibm_sa_vol", moduleIbmSaVol)
	r.Register("ibm_sa_vol_map", moduleIbmSaVolMap)

	// HPE 3PAR, via its own CLI over SSH.
	r.Register("ss_3par_cpg", moduleSs3parCpg)

	// HP iLO/RILOE, via hponcfg (not a substitution, the real module
	// already shells out to this exact binary).
	r.Register("hponcfg", moduleHponcfg)

	// Dell iDRAC Redfish, via racadm.
	r.Register("idrac_redfish_command", moduleIdracRedfishCommand)
	r.Register("idrac_redfish_config", moduleIdracRedfishConfig)
	r.Register("idrac_redfish_info", moduleIdracRedfishInfo)

	// HPE iLO Redfish, via ilorest.
	r.Register("ilo_redfish_command", moduleIloRedfishCommand)
	r.Register("ilo_redfish_config", moduleIloRedfishConfig)
	r.Register("ilo_redfish_info", moduleIloRedfishInfo)

	// Lenovo XCC Redfish, via OneCli.
	r.Register("xcc_redfish_command", moduleXccRedfishCommand)
}
