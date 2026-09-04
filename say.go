package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleSay implements Ansible's `say` (community.general) module:
// speaks msg aloud on the target via `say` (macOS) or `espeak`/
// `espeak-ng` (Linux) — whichever is found first on PATH, matching
// real say.py's own `for possible in ("say", "espeak", "espeak-ng"):
// executable = module.get_bin_path(possible)` search order exactly.
//
// Args: msg (required); voice (optional) — passed as `-v <voice>`;
// real say.py's own note that `say`'s own `-v` flag is silently
// dropped whenever platform.system() != "Darwin" (its own comment:
// "'say' binary available, it might be GNUstep tool which doesn't
// support 'voice' parameter") is NOT reproduced here — this port has
// no reliable way to learn the TARGET's own platform (Connection has
// no such primitive; see module.go's own doc comment on Exec being
// the whole surface), so `voice`, when given, is always passed to
// whichever of say/espeak/espeak-ng was found. A GNUstep `say` build
// on a non-Darwin target that doesn't understand `-v` would reject it
// — a narrow, documented gap, not a silent behavioral drift; real
// say.py's own workaround relies on `platform.system()`, which
// inspects the CONTROL machine the module runs ON in real Ansible
// (since real modules execute locally on their target), which this
// port's own architecture has no equivalent primitive for on the
// remote target.
//
// Always reports Changed=true on success, matching real say.py's own
// `module.exit_json(msg=msg, changed=True)` (speaking aloud is
// inherently non-idempotent — there is nothing to compare against).
func moduleSay(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	msg, err := requireString(args, "msg")
	if err != nil {
		return Result{}, err
	}
	voice := argString(args, "voice", "")

	bin, err := sayBin(ctx, conn)
	if err != nil {
		return Result{}, err
	}
	if bin == "" {
		return Fail("say: unable to find either say, espeak, espeak-ng"), nil
	}

	argv := []string{bin, msg}
	if voice != "" {
		argv = append(argv, "-v", voice)
	}
	quoted := make([]string, len(argv))
	for i, a := range argv {
		quoted[i] = shellQuote(a)
	}
	res, err := conn.Exec(ctx, strings.Join(quoted, " "), nil)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail("say: " + strings.TrimSpace(res.Stderr)), nil
	}
	return Changed(msg).WithExtra("msg", msg), nil
}

// sayBin returns the first of say/espeak/espeak-ng found on the
// target's PATH, or "" if none are, matching real say.py's own search
// order exactly.
func sayBin(ctx context.Context, conn remoteexec.Connection) (string, error) {
	for _, candidate := range []string{"say", "espeak", "espeak-ng"} {
		res, err := runStatus(ctx, conn, "command -v "+candidate)
		if err != nil {
			return "", err
		}
		if res.RC == 0 {
			return candidate, nil
		}
	}
	return "", nil
}
