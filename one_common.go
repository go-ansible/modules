package modules

import (
	"context"
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// This file factors out what the seven one_* modules in this batch
// share: shelling out to OpenNebula's own official per-resource CLI
// tools (onehost, oneimage, onetemplate, onevm, onevnet, plus
// oneflow/oneflow-template for the separate OneFlow service-orchestration
// component one_service manages) instead of speaking OpenNebula's own
// XML-RPC API through pyone the way every real one_* (community.general)
// module does — the same "shell out to the platform's own official CLI
// instead of an API client" precedent this port already uses for
// Consul/Redis/Terraform/Icinga2/Kopia/GitHub/GitLab/Keycloak/Rundeck in
// prior batches.
//
// # Verified real CLI tools, one binary per resource type
//
// Unlike a single unified `one` command, OpenNebula ships SEPARATE
// binaries per resource type — verified directly against
// github.com/OpenNebula/one's own src/cli/ source (onehost, oneimage,
// onetemplate, onevm, onevnet, oneflow, oneflow-template), not guessed
// from the module names. Every one of them accepts a `-x`/`--xml` flag
// on its `list`/`show` subcommands for structured XML output (`oneflow`/
// `oneflow-template` use `--json` instead — a different serialization
// their own CLI script defines, also verified against source), which
// this port decodes with a small generic XML->map decoder (oneXMLNode
// below) rather than a hand-written struct per resource: every
// resource's XML shape nests TEMPLATE/PERMISSIONS sub-objects
// arbitrarily, so a generic decoder is far less brittle than guessing
// a fixed schema this port has no live cluster to verify against.
//
// # Auth precondition
//
// Real one_* modules authenticate per-invocation via their own
// api_url/api_username/api_password (aliased api_token) arguments,
// opening a fresh pyone XML-RPC session each task run. The real `one*`
// CLI tools this port shells out to have a narrower story — verified
// against OpenNebula's own CLI reference docs: ONE_XMLRPC (the RPC
// endpoint URL — NOT a secret) and ONE_AUTH (a path to a file
// containing "username:password" or "username:token", defaulting to
// $HOME/.one/one_auth when unset) — there is no environment variable
// or flag for supplying a password/token value directly on the command
// line or in the process environment; ONE_AUTH always names a FILE.
// So, for every one_* module in this batch, matching the shape
// ipa_common.go's own doc comment already sets for a similar
// CLI-vs-API-client auth gap:
//   - api_url IS wired in, as the ONE_XMLRPC environment variable for
//     each invocation (not a secret, so this is a real improvement over
//     a dead argument, not a documented gap).
//   - api_username/api_password (aliased api_token) are accepted (for
//     argument-shape compatibility with real playbooks) but have NO
//     EFFECT on this port's behavior — this port does not write an
//     auth FILE on the target from a plaintext password argument (that
//     would itself be a secret-handling decision well beyond "shell out
//     to an already-configured CLI"). A valid $HOME/.one/one_auth file
//     (or ONE_AUTH pointing elsewhere) must already exist on the target
//     before this port's one_* modules run — this port does not manage
//     that file itself, the same way ipa_common.go does not drive
//     `kinit` and gitlab_common.go does not drive `glab auth login`.
//
// validate_certs is likewise accepted but has no effect (the `one*`
// CLI tools have no per-invocation TLS-verification flag this port
// could verify).

// oneRequireBinary fails cleanly (Result{Failed:true}, not a Go error)
// if the named OpenNebula CLI binary is not on the target's PATH.
func oneRequireBinary(ctx context.Context, conn remoteexec.Connection, bin, moduleName string) (Result, bool) {
	if _, err := run(ctx, conn, "command -v "+bin); err != nil {
		return Fail(fmt.Sprintf("%s: the %s binary (part of OpenNebula's own official CLI tools) is required on "+
			"the target and was not found in PATH — this port shells out to it rather than speaking OpenNebula's "+
			"XML-RPC API via pyone directly; see one_common.go's own doc comment, including the precondition "+
			"that a $HOME/.one/one_auth file (or ONE_AUTH pointing elsewhere) must already exist on the target",
			moduleName, bin)), false
	}
	return Result{}, true
}

// oneAuth pulls api_url out of args (see one_common.go's own doc
// comment on why api_username/api_password/api_token are accepted but
// unwired).
func oneAuth(args map[string]any) (url string) {
	return argString(args, "api_url", argString(args, "api_endpoint", ""))
}

// oneEnvPrefix renders the ONE_XMLRPC=<url> environment prefix for one
// invocation, or "" when url is empty (the CLI then falls back to its
// own default endpoint, matching real pyone's own ONE_URL-env-or-default
// fallback).
func oneEnvPrefix(url string) string {
	if url == "" {
		return ""
	}
	return "ONE_XMLRPC=" + shellQuote(url) + " "
}

// oneRun runs one `<bin> <argv...>` invocation on conn, with
// ONE_XMLRPC set for that single command when url is non-empty.
func oneRun(ctx context.Context, conn remoteexec.Connection, url, bin string, argv ...string) (remoteexec.Result, error) {
	quoted := make([]string, len(argv))
	for i, a := range argv {
		quoted[i] = shellQuote(a)
	}
	cmd := oneEnvPrefix(url) + bin + " " + strings.Join(quoted, " ")
	return conn.Exec(ctx, cmd, nil)
}

// oneRunStdin is oneRun but pipes body to the command's stdin (used for
// the "-" file-argument convention onetemplate/oneflow-template/onehost/
// onevnet's own `update`/`instantiate` subcommands support for reading
// extra template content from stdin instead of a named file).
func oneRunStdin(ctx context.Context, conn remoteexec.Connection, url, bin, body string, argv ...string) (remoteexec.Result, error) {
	quoted := make([]string, len(argv))
	for i, a := range argv {
		quoted[i] = shellQuote(a)
	}
	cmd := oneEnvPrefix(url) + bin + " " + strings.Join(quoted, " ")
	return conn.Exec(ctx, cmd, strings.NewReader(body))
}

// oneErrMsg builds a Fail() message from a non-zero `one*` CLI result,
// preferring stderr but falling back to stdout.
func oneErrMsg(res remoteexec.Result) string {
	msg := strings.TrimSpace(res.Stderr)
	if msg == "" {
		msg = strings.TrimSpace(res.Stdout)
	}
	return msg
}

// oneXMLNode is a generic XML element: every child, of any tag, is
// captured recursively — used to decode any OpenNebula `-x`/`--xml`
// resource representation (HOST/IMAGE/VM/VNET/VMTEMPLATE, each pool
// wrapper, each nested TEMPLATE/PERMISSIONS block) without a
// hand-written struct per resource. See one_common.go's own doc
// comment on why a generic decoder was chosen over guessing a fixed
// schema this port has no live cluster to verify against.
type oneXMLNode struct {
	XMLName xml.Name
	Content string       `xml:",chardata"`
	Nodes   []oneXMLNode `xml:",any"`
}

// oneParseXML decodes data as a single oneXMLNode tree rooted at its
// outermost element.
func oneParseXML(data string) (oneXMLNode, error) {
	var n oneXMLNode
	if err := xml.Unmarshal([]byte(data), &n); err != nil {
		return oneXMLNode{}, fmt.Errorf("decoding OpenNebula XML: %w", err)
	}
	return n, nil
}

// child returns n's first direct child with the given tag.
func (n oneXMLNode) child(tag string) (oneXMLNode, bool) {
	for _, c := range n.Nodes {
		if c.XMLName.Local == tag {
			return c, true
		}
	}
	return oneXMLNode{}, false
}

// children returns every direct child of n with the given tag.
func (n oneXMLNode) children(tag string) []oneXMLNode {
	var out []oneXMLNode
	for _, c := range n.Nodes {
		if c.XMLName.Local == tag {
			out = append(out, c)
		}
	}
	return out
}

// text returns n's own trimmed character content.
func (n oneXMLNode) text() string { return strings.TrimSpace(n.Content) }

// childText is child(tag).text(), or "" if tag is absent.
func (n oneXMLNode) childText(tag string) string {
	c, ok := n.child(tag)
	if !ok {
		return ""
	}
	return c.text()
}

// childInt is childText(tag) parsed as an int, or 0 on any failure.
func (n oneXMLNode) childInt(tag string) int {
	v, _ := strconv.Atoi(n.childText(tag))
	return v
}

// toMap converts n's own direct children into a map[string]any: a leaf
// child (no children of its own) becomes its trimmed text; a non-leaf
// child becomes its own recursively-converted map; a repeated tag
// becomes a []any of those values, matching the generic JSON shape
// this project's other _common.go files already decode CLI JSON output
// into (e.g. glab_common.go's own map[string]any facts).
func (n oneXMLNode) toMap() map[string]any {
	out := map[string]any{}
	for _, c := range n.Nodes {
		var val any
		if len(c.Nodes) == 0 {
			val = c.text()
		} else {
			val = c.toMap()
		}
		key := c.XMLName.Local
		if existing, ok := out[key]; ok {
			if list, ok := existing.([]any); ok {
				out[key] = append(list, val)
			} else {
				out[key] = []any{existing, val}
			}
		} else {
			out[key] = val
		}
	}
	return out
}

// oneShowXML runs `<bin> show <id> -x` and decodes the result. found is
// false (nil error) on a non-zero exit, matching kcadm_common.go's own
// kcadmShow convention: a missing OpenNebula resource is an expected,
// common outcome, not an infrastructure failure.
func oneShowXML(ctx context.Context, conn remoteexec.Connection, url, bin, id string) (node oneXMLNode, found bool, err error) {
	res, err := oneRun(ctx, conn, url, bin, "show", id, "-x")
	if err != nil {
		return oneXMLNode{}, false, err
	}
	if res.RC != 0 || strings.TrimSpace(res.Stdout) == "" {
		return oneXMLNode{}, false, nil
	}
	node, err = oneParseXML(res.Stdout)
	if err != nil {
		return oneXMLNode{}, false, err
	}
	return node, true, nil
}

// oneListXML runs `<bin> list -x` and decodes the resulting pool.
func oneListXML(ctx context.Context, conn remoteexec.Connection, url, bin string) (oneXMLNode, error) {
	res, err := oneRun(ctx, conn, url, bin, "list", "-x")
	if err != nil {
		return oneXMLNode{}, err
	}
	if res.RC != 0 {
		return oneXMLNode{}, fmt.Errorf("%s list: %s", bin, oneErrMsg(res))
	}
	if strings.TrimSpace(res.Stdout) == "" {
		return oneXMLNode{}, nil
	}
	return oneParseXML(res.Stdout)
}

// oneResolveByName searches pool's direct children with itemTag for one
// whose own NAME element equals name.
func oneResolveByName(pool oneXMLNode, itemTag, name string) (oneXMLNode, bool) {
	for _, item := range pool.children(itemTag) {
		if item.childText("NAME") == name {
			return item, true
		}
	}
	return oneXMLNode{}, false
}

// oneIDOrName resolves an id/name pair the way every one_* module in
// this batch accepts them (an "id" int arg, or a "name" string arg): if
// id is set, it is used directly (no lookup needed, matching real
// one_image.py's own `id`-takes-precedence-over-`name` ordering); else
// name is resolved against a freshly-listed pool via oneResolveByName.
// found=false (nil error) when neither resolves to an existing resource.
func oneIDOrName(ctx context.Context, conn remoteexec.Connection, url, bin, itemTag string, args map[string]any) (id string, found bool, err error) {
	if v, ok := args["id"]; ok && v != nil {
		s := fmtAny(v)
		_, exists, err := oneShowXML(ctx, conn, url, bin, s)
		return s, exists, err
	}
	name := argString(args, "name", "")
	if name == "" {
		return "", false, nil
	}
	pool, err := oneListXML(ctx, conn, url, bin)
	if err != nil {
		return "", false, err
	}
	item, ok := oneResolveByName(pool, itemTag, name)
	if !ok {
		return "", false, nil
	}
	return item.childText("ID"), true, nil
}
