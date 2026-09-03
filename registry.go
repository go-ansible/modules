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
}
