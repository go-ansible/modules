package modules

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
}
