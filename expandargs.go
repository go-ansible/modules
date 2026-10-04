package modules

import "strings"

// expandArgumentVars is real Ansible's `expand_argument_vars` for
// `command` (default true since 2.16), which run_command implements as
//
//	os.path.expanduser(os.path.expandvars(x))
//
// on every argv element when no shell is used. Measured against
// ansible-core 2.21.4: `command: echo $HOME *` prints the home
// directory and a LITERAL asterisk -- variables expand, globs do not --
// while this port printed `$HOME *`, expanding neither.
//
// ⛔ The hard part is WHERE. Real's module runs on the target, so $HOME
// is the TARGET's. This port's modules run on the controller, so
// expanding locally would substitute the controller's home into a
// command meant for another machine -- a wrong answer that looks right
// whenever both are the same user, which is exactly how it would reach
// production.
//
// So the expansion is DELEGATED to the target's shell, and this function
// decides only WHICH spans may be delegated. Everything else stays
// shellQuote'd, so no glob, no `$(...)`, no backtick and no word split
// can reach the shell from an argument. The only unquoted text ever
// emitted is `"${NAME-\$NAME}"` for a name matching [A-Za-z_][A-Za-z0-9_]*:
//
//   - the braces and the double quotes stop globbing and word splitting;
//   - the `-` default makes an UNSET name expand to the literal `$NAME`,
//     which is real's documented behaviour ("if a variable is not
//     matched, it is left unchanged, unlike shell substitution which
//     would remove it") and the opposite of what a bare "$NAME" does.
//
// A leading `~` becomes `"${HOME-~}"`. Real's expanduser consults the
// password database when HOME is unset, where this falls back to the
// literal `~`; that difference needs an account whose HOME is unset to
// observe, and it is named rather than papered over.
func expandArgumentVars(arg string) string {
	var b strings.Builder
	rest := arg

	// A `~` expands only at the START of an argument, which is what
	// expanduser does -- "a~b" is not a home directory.
	if strings.HasPrefix(rest, "~") && (len(rest) == 1 || rest[1] == '/') {
		b.WriteString(`"${HOME-~}"`)
		rest = rest[1:]
	}

	for {
		i := strings.IndexByte(rest, '$')
		if i < 0 {
			break
		}
		name, after, ok := varRefAt(rest[i:])
		if !ok {
			// Not a name we will delegate -- `$(`, `$1`, a lone `$`.
			// Keep it, quoted, as literal text: real's expandvars
			// leaves these alone too.
			b.WriteString(shellQuote(rest[:i+1]))
			rest = rest[i+1:]
			continue
		}
		if i > 0 {
			b.WriteString(shellQuote(rest[:i]))
		}
		b.WriteString(`"${` + name + `-\$` + name + `}"`)
		rest = after
	}
	b.WriteString(shellQuote(rest))
	return b.String()
}

// varRefAt reads a $NAME or ${NAME} reference at the start of s and
// returns the name and what follows. Anything else -- $(, $1, ${1},
// a bare $ -- is not a reference this will delegate.
func varRefAt(s string) (name, rest string, ok bool) {
	if len(s) < 2 || s[0] != '$' {
		return "", "", false
	}
	body := s[1:]
	braced := false
	if body[0] == '{' {
		braced = true
		body = body[1:]
	}
	n := 0
	for n < len(body) {
		c := body[n]
		if c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (n > 0 && c >= '0' && c <= '9') {
			n++
			continue
		}
		break
	}
	if n == 0 {
		return "", "", false
	}
	name = body[:n]
	rest = body[n:]
	if braced {
		// ${NAME} only. ${NAME:-x}, ${#NAME} and friends are shell
		// syntax real's expandvars does not implement, so they stay
		// literal.
		if !strings.HasPrefix(rest, "}") {
			return "", "", false
		}
		rest = rest[1:]
	}
	return name, rest, true
}
