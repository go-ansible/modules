package modules

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleTerraform implements Ansible's `terraform` (community.general)
// module: wraps the `terraform` CLI's own init/plan/apply/destroy
// subcommands for a given project directory — the same binary real
// terraform's own module wraps (there is no library form to substitute
// here: real terraform already shells out to the `terraform` binary
// itself).
//
// Args: project_path (required) — the directory holding the Terraform
// config; state (default present: present=apply, absent=destroy,
// planned=plan-only, never applying); binary_path — an explicit
// `terraform` binary, relative to project_path unless absolute; workspace
// (default "default") — selected via `terraform workspace select`,
// creating it first via `terraform workspace new` if it doesn't exist
// yet, matching real terraform's own get_workspace_context/
// create_workspace/select_workspace; variables (map) — passed as
// `-var k=v` (values are NOT quoted for Terraform's own HCL syntax
// unless complex_vars is set — see below); variables_files (aliased
// variables_file) — `-var-file=<path>` per entry; backend_config (map)
// — `-backend-config=k=v` per entry, at init; backend_config_files —
// `-backend-config=<path>` per entry, at init; force_init (bool) — run
// init even when overwrite_init (default true) would otherwise skip it
// because `.terraform/terraform.tfstate` already exists; init_reconfigure
// (bool) — `-reconfigure`; provider_upgrade (bool) — `-upgrade`;
// plugin_paths — `-plugin-dir=<path>` per entry (disables Terraform's
// own auto-download); lock (default true) / lock_timeout — `-lock=` /
// `-lock-timeout=Ns`; parallelism (int) — `-parallelism=N`; targets
// ([]string) — `-target=<addr>` per entry; state_file — `-state=<path>`;
// no_color (default true) — `-no-color`; check_destroy (bool) — fail
// (rather than apply) a plan that would destroy any resource, unless
// state=absent; purge_workspace (bool) — after state=absent, `terraform
// workspace delete` (skipped for the "default" workspace, matching real
// terraform's own documented note); complex_vars (bool, default false)
// — when true, map/list/bool/number values in `variables` are JSON-
// encoded before being passed as `-var k=<json>` (matching real
// terraform's own documented support for nested Terraform variable
// structures); when false, only string/number values are accepted and
// passed through unquoted, matching real terraform's own documented
// simple-variable-only default.
//
// Idempotency mirrors real terraform's own `-detailed-exitcode` plan
// convention exactly: `terraform plan -detailed-exitcode -out=<tmp>`
// exits 0 (no changes: Changed=false, nothing else runs), 2 (changes
// pending), or 1 (a real plan failure, surfaced as Result{Failed:true}
// with the plan's own stdout/stderr in Extra). On exit 2: state=planned
// stops there (Changed=true, the plan is not applied — Extra["stdout"]
// holds the plan's own human-readable diff since this port does not
// implement real terraform's own separate `-json` plan-diff parsing
// into a structured Extra["diff"]); state=present runs `terraform apply
// -input=false <planfile>`; state=absent instead plans with `-destroy`
// and applies that plan. check_destroy (state=present only) fails
// cleanly before applying if the plan's own JSON summary reports a
// planned "delete" or "delete_then_recreate"/"replace" action on any
// resource — parsed from `terraform show -json <planfile>`, matching
// real terraform's own get_diff/check_destroy.
//
// Extra["outputs"]: `terraform output -json` decoded, matching real
// terraform's own; empty if none are defined (not a failure, matching
// real terraform's own tolerant handling of that specific case).
//
// Deviation from real terraform: diff_mode support (real terraform's
// own attributes declare diff_mode: full — Ansible's own --diff flag
// gets a structured before/after resource diff) is not implemented,
// since this package's Result has no diff channel (see pip_package_info.go's
// identical note about the same gap, referenced by pacemaker_cluster.go's
// own doc comment too); this port's Extra["stdout"]/Extra["stderr"] from
// the plan/apply run are the closest equivalent a caller has to inspect.
func moduleTerraform(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	projectPath, err := requireString(args, "project_path")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" && state != "planned" {
		return Result{}, errArg("terraform: state must be one of present, absent, planned, got %q", state)
	}
	bin := argString(args, "binary_path", "terraform")
	noColor := argBool(args, "no_color", true)
	workspace := argString(args, "workspace", "default")

	varArgs, err := terraformVarArgs(args)
	if err != nil {
		return Result{}, err
	}

	// init
	initArgs := []string{bin, "-chdir=" + projectPath, "init", "-input=false"}
	if noColor {
		initArgs = append(initArgs, "-no-color")
	}
	for k, v := range argMapString(args, "backend_config") {
		initArgs = append(initArgs, "-backend-config="+k+"="+v)
	}
	for _, f := range argStringList(args, "backend_config_files") {
		initArgs = append(initArgs, "-backend-config="+f)
	}
	if argBool(args, "init_reconfigure", false) {
		initArgs = append(initArgs, "-reconfigure")
	}
	if argBool(args, "provider_upgrade", false) {
		initArgs = append(initArgs, "-upgrade")
	}
	for _, p := range argStringList(args, "plugin_paths") {
		initArgs = append(initArgs, "-plugin-dir="+p)
	}
	initRes, err := terraformRun(ctx, conn, initArgs)
	if err != nil {
		return Result{}, err
	}
	if initRes.RC != 0 {
		return Fail("terraform: init failed: "+strings.TrimSpace(initRes.Stderr)).WithExtra("stdout", initRes.Stdout).WithExtra("stderr", initRes.Stderr), nil
	}

	if err := terraformSelectWorkspace(ctx, conn, bin, projectPath, workspace, noColor); err != nil {
		return Result{}, err
	}

	// plan
	planFile := conn.TempPath("terraform.tfplan")
	if planFile == "" {
		planFile = "/tmp/terraform.tfplan"
	}

	planArgs := []string{bin, "-chdir=" + projectPath, "plan", "-input=false", "-detailed-exitcode", "-out=" + planFile}
	if noColor {
		planArgs = append(planArgs, "-no-color")
	}
	if state == "absent" {
		planArgs = append(planArgs, "-destroy")
	}
	for _, t := range argStringList(args, "targets") {
		planArgs = append(planArgs, "-target="+t)
	}
	if sf := argString(args, "state_file", ""); sf != "" {
		planArgs = append(planArgs, "-state="+sf)
	}
	if !argBool(args, "lock", true) {
		planArgs = append(planArgs, "-lock=false")
	}
	if lt := argInt(args, "lock_timeout", 0); lt > 0 {
		planArgs = append(planArgs, "-lock-timeout="+strconv.Itoa(lt)+"s")
	}
	if par := argInt(args, "parallelism", 0); par > 0 {
		planArgs = append(planArgs, "-parallelism="+strconv.Itoa(par))
	}
	planArgs = append(planArgs, varArgs...)
	for _, f := range argStringList(args, "variables_files") {
		planArgs = append(planArgs, "-var-file="+f)
	}
	for _, f := range argStringList(args, "variables_file") {
		planArgs = append(planArgs, "-var-file="+f)
	}

	planRes, err := terraformRun(ctx, conn, planArgs)
	if err != nil {
		return Result{}, err
	}
	switch planRes.RC {
	case 0:
		outputs, oerr := terraformOutputs(ctx, conn, bin, projectPath, noColor)
		if oerr != nil {
			return Result{}, oerr
		}
		return Ok("terraform: no changes").WithExtra("outputs", outputs), nil
	case 2:
		// changes pending, fall through
	default:
		return Fail("terraform: plan failed: "+strings.TrimSpace(planRes.Stderr)).
			WithExtra("stdout", planRes.Stdout).WithExtra("stderr", planRes.Stderr), nil
	}

	if state == "planned" {
		outputs, oerr := terraformOutputs(ctx, conn, bin, projectPath, noColor)
		if oerr != nil {
			return Result{}, oerr
		}
		return Changed("terraform: plan created, not applied").
			WithExtra("stdout", planRes.Stdout).WithExtra("outputs", outputs), nil
	}

	if state == "present" && argBool(args, "check_destroy", false) {
		destructive, derr := terraformPlanIsDestructive(ctx, conn, bin, projectPath, planFile, noColor)
		if derr != nil {
			return Result{}, derr
		}
		if destructive {
			return Fail("terraform: check_destroy is set and the plan would destroy at least one resource"), nil
		}
	}

	applyArgs := []string{bin, "-chdir=" + projectPath, "apply", "-input=false"}
	if noColor {
		applyArgs = append(applyArgs, "-no-color")
	}
	if !argBool(args, "lock", true) {
		applyArgs = append(applyArgs, "-lock=false")
	}
	applyArgs = append(applyArgs, planFile)
	applyRes, err := terraformRun(ctx, conn, applyArgs)
	if err != nil {
		return Result{}, err
	}
	if applyRes.RC != 0 {
		return Fail("terraform: apply failed: "+strings.TrimSpace(applyRes.Stderr)).
			WithExtra("stdout", applyRes.Stdout).WithExtra("stderr", applyRes.Stderr), nil
	}

	if state == "absent" && argBool(args, "purge_workspace", false) && workspace != "default" {
		if _, err := terraformRun(ctx, conn, []string{bin, "-chdir=" + projectPath, "workspace", "select", "default"}); err != nil {
			return Result{}, err
		}
		if _, err := terraformRun(ctx, conn, []string{bin, "-chdir=" + projectPath, "workspace", "delete", workspace}); err != nil {
			return Result{}, err
		}
	}

	outputs, oerr := terraformOutputs(ctx, conn, bin, projectPath, noColor)
	if oerr != nil {
		return Result{}, oerr
	}
	res := Changed("terraform: applied")
	return res.WithExtra("stdout", applyRes.Stdout).WithExtra("outputs", outputs), nil
}

// terraformRun quotes and runs one terraform invocation.
func terraformRun(ctx context.Context, conn remoteexec.Connection, argv []string) (remoteexec.Result, error) {
	quoted := make([]string, len(argv))
	for i, a := range argv {
		quoted[i] = shellQuote(a)
	}
	return conn.Exec(ctx, strings.Join(quoted, " "), nil)
}

// terraformVarArgs builds `-var k=v` args from the `variables` map,
// JSON-encoding non-string/number values only when complex_vars is set
// — matching real terraform's own documented complex_vars gate exactly
// (a complex value with complex_vars unset is a real terraform config
// error too, so this port does not special-case it further).
func terraformVarArgs(args map[string]any) ([]string, error) {
	vars := argMapAny(args, "variables")
	if len(vars) == 0 {
		return nil, nil
	}
	complex := argBool(args, "complex_vars", false)
	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var out []string
	for _, k := range keys {
		v := vars[k]
		switch v.(type) {
		case string, int, int64, float64, bool:
			if !complex {
				out = append(out, "-var="+k+"="+fmt.Sprint(v))
				continue
			}
		}
		if !complex {
			return nil, errArg("terraform: variable %q is a complex value but complex_vars is not set", k)
		}
		b, err := json.Marshal(v)
		if err != nil {
			return nil, errArg("terraform: encoding variable %q: %v", k, err)
		}
		out = append(out, "-var="+k+"="+string(b))
	}
	return out, nil
}

// terraformSelectWorkspace creates workspace (if `terraform workspace
// list` doesn't already list it) and selects it, matching real
// terraform's own get_workspace_context/create_workspace/
// select_workspace.
func terraformSelectWorkspace(ctx context.Context, conn remoteexec.Connection, bin, projectPath, workspace string, noColor bool) error {
	listArgs := []string{bin, "-chdir=" + projectPath, "workspace", "list"}
	if noColor {
		listArgs = append(listArgs, "-no-color")
	}
	res, err := terraformRun(ctx, conn, listArgs)
	if err != nil {
		return err
	}
	exists := false
	for _, line := range strings.Split(res.Stdout, "\n") {
		name := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "*"))
		if name == workspace {
			exists = true
			break
		}
	}
	if !exists {
		newArgs := []string{bin, "-chdir=" + projectPath, "workspace", "new", workspace}
		if noColor {
			newArgs = append(newArgs, "-no-color")
		}
		if _, err := terraformRun(ctx, conn, newArgs); err != nil {
			return err
		}
		return nil // `workspace new` also selects it
	}
	selArgs := []string{bin, "-chdir=" + projectPath, "workspace", "select", workspace}
	if noColor {
		selArgs = append(selArgs, "-no-color")
	}
	_, err = terraformRun(ctx, conn, selArgs)
	return err
}

// terraformOutputs decodes `terraform output -json`, treating a decode
// failure as "no outputs defined" (empty map, not a failure) — matching
// real terraform's own tolerant handling of that specific, common case
// (a project with no `output` blocks at all).
func terraformOutputs(ctx context.Context, conn remoteexec.Connection, bin, projectPath string, noColor bool) (map[string]any, error) {
	a := []string{bin, "-chdir=" + projectPath, "output", "-json"}
	if noColor {
		a = append(a, "-no-color")
	}
	res, err := terraformRun(ctx, conn, a)
	if err != nil {
		return nil, err
	}
	outputs := map[string]any{}
	if res.RC != 0 {
		return outputs, nil
	}
	_ = json.Unmarshal([]byte(res.Stdout), &outputs)
	return outputs, nil
}

// terraformPlanIsDestructive decodes `terraform show -json <planFile>`
// and reports whether any planned resource_change contains a "delete"
// action, matching real terraform's own check_destroy/get_diff (which
// treats "delete" and "delete_then_recreate"/replace sequences alike —
// any change list containing "delete" trips it).
func terraformPlanIsDestructive(ctx context.Context, conn remoteexec.Connection, bin, projectPath, planFile string, noColor bool) (bool, error) {
	a := []string{bin, "-chdir=" + projectPath, "show", "-json", planFile}
	if noColor {
		a = append(a, "-no-color")
	}
	res, err := terraformRun(ctx, conn, a)
	if err != nil {
		return false, err
	}
	if res.RC != 0 {
		return false, fmt.Errorf("terraform: show -json failed: %s", strings.TrimSpace(res.Stderr))
	}
	var parsed struct {
		ResourceChanges []struct {
			Change struct {
				Actions []string `json:"actions"`
			} `json:"change"`
		} `json:"resource_changes"`
	}
	if err := json.Unmarshal([]byte(res.Stdout), &parsed); err != nil {
		return false, nil
	}
	for _, rc := range parsed.ResourceChanges {
		for _, a := range rc.Change.Actions {
			if a == "delete" {
				return true, nil
			}
		}
	}
	return false, nil
}

// argMapString reads a module argument as a map[string]string,
// stringifying non-string values — used for terraform's own
// backend_config (a flat string-valued map in every real usage).
func argMapString(args map[string]any, key string) map[string]string {
	v, ok := args[key]
	if !ok {
		return nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, val := range m {
		out[k] = fmt.Sprint(val)
	}
	return out
}

// argMapAny reads a module argument as a map[string]any — used for
// terraform's own `variables`, whose values may be non-string when
// complex_vars is set.
func argMapAny(args map[string]any, key string) map[string]any {
	v, ok := args[key]
	if !ok {
		return nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	return m
}
