package modules

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleCirconusAnnotation implements Ansible's `circonus_annotation`
// (community.general) module: creates a Circonus annotation event
// (category/title/description, optional start/stop/duration) via
// Circonus's own official CLI, `circli`
// (github.com/circonus-labs/circonus-api-cli, `circonus-labs` being
// Circonus's own official GitHub org) — instead of real
// circonus_annotation.py's own bare `requests.post` to
// `https://api.circonus.com/v2/annotation`.
//
// # CLI syntax, verified against circli's own README
//
// circli's own generic object-management shape (confirmed directly,
// not guessed from the module name) is
// `circli -object <type> -call <verb> [-where <json> | -file <path>]`
// — a JSON-formatted-flag-value passthrough onto Circonus's own API
// objects, the same "no resource-specific verb, every call takes a
// JSON payload" shape memset_common.go's own doc comment already
// documents for `ma-shell` (a different platform, same class of CLI).
// For creating an annotation specifically, circli's own README example
// is:
//
//	circli -object annotation -call create -where '{"title":"Test","category":"maintenance","description":"Window","start":1467047744,"stop":1467048744}'
//
// This port uses `-where` (an inline JSON string) rather than `-file`
// (a JSON file this port would have to Put to the target first, for no
// benefit here, since the payload is small and has no embedded
// secret).
//
// # Auth, verified against circli's own README
//
// circli's own README documents THREE credential sources: the
// `CIRCONUS_API_TOKEN` environment variable (its own "Recommended"
// option), a `-api_token` command-line flag, or a sourced shell-config
// file. Per this project's hard "no secrets in argv" rule, this port
// always uses the environment-variable form, matching xcli's/twilio-
// cli's/ma-shell's-lack-thereof precedents in this batch — except this
// one genuinely HAS a documented env var, unlike ma-shell, so this
// port uses it rather than accepting an argv-secret deviation.
//
// Args: api_key (required) → CIRCONUS_API_TOKEN; category (required);
// title (required); description (required); start (optional Unix
// timestamp, defaults to "now" — computed by THIS PORT at call time,
// matching real circonus_annotation.py's own `int(time.time())`
// default exactly, since circli itself has no "now" keyword
// documented); stop (optional, defaults to start+duration, same
// reasoning); duration (default 0, only consulted when stop is unset,
// matching real create_annotation()'s own logic exactly).
//
// Deviation — non-idempotent, matching real circonus_annotation.py's
// own behavior (it always POSTs a brand new annotation object, with no
// existence check of any kind): this port always reports Changed=true
// on a zero exit.
func moduleCirconusAnnotation(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	const mod = "circonus_annotation"
	apiKey, err := requireString(args, "api_key")
	if err != nil {
		return Result{}, err
	}
	category, err := requireString(args, "category")
	if err != nil {
		return Result{}, err
	}
	title, err := requireString(args, "title")
	if err != nil {
		return Result{}, err
	}
	description, err := requireString(args, "description")
	if err != nil {
		return Result{}, err
	}
	duration := argInt(args, "duration", 0)

	start := argInt(args, "start", int(time.Now().Unix()))
	stop := argInt(args, "stop", start+duration)

	if res, ok := circonusRequireBinary(ctx, conn, mod); !ok {
		return res, nil
	}

	payload := map[string]any{
		"category":    category,
		"title":       title,
		"description": description,
		"start":       start,
		"stop":        stop,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return Result{}, fmt.Errorf("%s: encoding annotation payload: %w", mod, err)
	}

	cmd := "CIRCONUS_API_TOKEN=" + shellQuote(apiKey) + " circli " +
		strings.Join([]string{"-object", "annotation", "-call", "create", "-where", shellQuote(string(body))}, " ")
	res, err := conn.Exec(ctx, cmd, nil)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail(mod + ": circli failed to create annotation: " + circonusErrMsg(res)), nil
	}

	var annotation map[string]any
	out := strings.TrimSpace(res.Stdout)
	if out != "" {
		if jerr := json.Unmarshal([]byte(out), &annotation); jerr == nil {
			return Changed("").WithExtra("annotation", annotation), nil
		}
	}
	return Changed("").WithExtra("annotation", out), nil
}

func circonusRequireBinary(ctx context.Context, conn remoteexec.Connection, moduleName string) (Result, bool) {
	if _, err := run(ctx, conn, "command -v circli"); err != nil {
		return Fail(fmt.Sprintf("%s: the circli binary (Circonus's own official CLI, circonus-api-cli) is "+
			"required on the target and was not found in PATH — this port shells out to it rather than POSTing "+
			"to Circonus's v2 API directly; see moduleCirconusAnnotation's own doc comment", moduleName)), false
	}
	return Result{}, true
}

func circonusErrMsg(res remoteexec.Result) string {
	msg := strings.TrimSpace(res.Stderr)
	if msg == "" {
		msg = strings.TrimSpace(res.Stdout)
	}
	return msg
}
