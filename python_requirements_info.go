package modules

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// modulePythonRequirementsInfo implements Ansible's
// `python_requirements_info` module: reports the target's Python
// interpreter details and checks a list of dependency version
// specifiers against what is actually installed, read-only.
//
// Args: dependencies ([]string, default []) — bare module names
// ("ansible"), pinned ("boto3==1.6.1"), or partial ("requests>2")
// specifiers; supported operators: ==, >=, <=, !=, >, <.
//
// Never Changed — this module only ever reads.
//
// Real python_requirements_info runs AS a Python module in Ansible's
// own module execution model: it directly introspects `sys.executable`,
// `sys.version_info`, `sys.path`, and each dependency's installed
// version via `pkg_resources`/`importlib.metadata` — all from WITHIN
// the very same Python process real Ansible used to run the module
// itself, so "the interpreter this info describes" and "the interpreter
// that gathered it" are trivially the same one. This port has no
// embedded Python interpreter and no equivalent of Ansible's own
// module-execution Python process — the only Python this port can
// describe is one on the TARGET, reached via a plain `python3 -c`
// command composed and run through the Connection, the same
// "compose a command, run it via Exec" shape every other module in
// this package uses (never copying a script file to the target). This
// is a real, disclosed architectural narrowing: this port always probes
// "python3" specifically (real python_requirements_info has no such
// hardcoded choice — it uses whatever interpreter Ansible itself was
// configured to run modules with, via ansible_python_interpreter, a
// facility this port does not have), and dependency versions are read
// via `importlib.metadata.version()` (installed package METADATA), not
// real python_requirements_info's own `pkg_resources`-based check
// (which for a couple of dependencies also falls back to a live
// `import` and a module's own `__version__` attribute when no package
// metadata exists) — a distribution installed without proper metadata,
// or importable only via a non-standard mechanism, is reported as
// not_found here where real python_requirements_info might still find
// it. Version comparison for the operator check uses a small
// dotted-numeric comparator (see the embedded Python's cmp_versions),
// not full PEP 440 semantics (pre-release/post-release/local-version
// segments) the way real python_requirements_info's `pkg_resources`
// comparison does — sufficient for the common "X.Y.Z" pins shown in
// python_requirements_info's own examples, not a byte-for-byte match
// for an exotic version string.
func modulePythonRequirementsInfo(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	deps := argStringList(args, "dependencies")

	cmdArgs := make([]string, 0, len(deps)+1)
	cmdArgs = append(cmdArgs, "python3", "-c", shellQuote(pythonRequirementsInfoScript))
	for _, d := range deps {
		cmdArgs = append(cmdArgs, shellQuote(d))
	}
	cmd := strings.Join(cmdArgs, " ")

	res, err := runStatus(ctx, conn, cmd)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail(fmt.Sprintf("python_requirements_info: running python3: %s", strings.TrimSpace(res.Stderr))), nil
	}

	var out struct {
		Python            string         `json:"python"`
		PythonVersion     string         `json:"python_version"`
		PythonVersionInfo map[string]any `json:"python_version_info"`
		PythonSystemPath  []any          `json:"python_system_path"`
		Valid             map[string]any `json:"valid"`
		Mismatched        map[string]any `json:"mismatched"`
		NotFound          []any          `json:"not_found"`
	}
	if err := json.Unmarshal([]byte(res.Stdout), &out); err != nil {
		return Fail(fmt.Sprintf("python_requirements_info: parsing python3 output: %v", err)), nil
	}

	result := Ok("")
	result = result.WithExtra("python", out.Python).
		WithExtra("python_version", out.PythonVersion).
		WithExtra("python_version_info", out.PythonVersionInfo).
		WithExtra("python_system_path", out.PythonSystemPath).
		WithExtra("valid", out.Valid).
		WithExtra("mismatched", out.Mismatched).
		WithExtra("not_found", out.NotFound)
	return result, nil
}

// pythonRequirementsInfoScript is run on the target as `python3 -c
// <script> <dependency-spec>...`. It prints one JSON object on stdout
// carrying every field modulePythonRequirementsInfo needs. Kept as a
// single inline script (rather than several round trips) so this
// module needs exactly one Exec call.
const pythonRequirementsInfoScript = `
import sys, json, re
try:
    from importlib import metadata as _md
except ImportError:
    import importlib_metadata as _md

def parse_spec(spec):
    m = re.match(r'^([A-Za-z0-9_.\-]+)\s*(==|>=|<=|!=|>|<)?\s*(.*)$', spec)
    if not m:
        return spec, None, None
    name, op, ver = m.groups()
    if not op:
        return name, None, None
    return name, op, ver

def norm(v):
    out = []
    for p in re.split(r'[.\-]', v):
        try:
            out.append((0, int(p)))
        except ValueError:
            out.append((1, p))
    return out

def cmp_versions(a, b):
    na, nb = norm(a), norm(b)
    return (na > nb) - (na < nb)

valid = {}
mismatched = {}
not_found = []

for spec in sys.argv[1:]:
    name, op, ver = parse_spec(spec)
    try:
        installed = _md.version(name)
    except Exception:
        not_found.append(name)
        continue
    if op is None:
        valid[name] = {"installed": installed, "desired": None}
        continue
    c = cmp_versions(installed, ver)
    ok = {"==": c == 0, ">=": c >= 0, "<=": c <= 0, ">": c > 0, "<": c < 0, "!=": c != 0}[op]
    if ok:
        valid[name] = {"installed": installed, "desired": spec}
    else:
        mismatched[name] = {"installed": installed, "desired": spec}

vi = sys.version_info
result = {
    "python": sys.executable,
    "python_version": sys.version,
    "python_version_info": {
        "major": vi.major,
        "minor": vi.minor,
        "micro": vi.micro,
        "releaselevel": vi.releaselevel,
        "serial": vi.serial,
    },
    "python_system_path": sys.path,
    "valid": valid,
    "mismatched": mismatched,
    "not_found": not_found,
}
print(json.dumps(result))
`
