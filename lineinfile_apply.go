package modules

import (
	"fmt"
	"strconv"
	"strings"

	pcre "github.com/go-regexp/engine"
)

// lineinfileSpec is one lineinfile edit, already parsed and validated.
type lineinfileSpec struct {
	line         string
	re           *pcre.Regexp // regexp:
	searchString string       // search_string:
	insertAfter  string       // insertafter:, or "EOF"/"BOF"
	insertBefore string       // insertbefore:, or "BOF"
	insAfterRe   *pcre.Regexp // compiled insertafter, unless EOF/BOF
	insBeforeRe  *pcre.Regexp // compiled insertbefore, unless BOF
	backrefs     bool
	firstmatch   bool
	state        string
}

// expandBackrefs is Python's match.expand(): \1..\9 and \g<N>/\g<name>
// become the matched group, \\ is a literal backslash. Real lineinfile
// calls it on the `line` when backrefs is set, so `line: 'alpha=[\1]'`
// against `^alpha=(.*)` produces alpha=[3] from alpha=3 -- measured
// against ansible-core 2.21.4.
func expandBackrefs(re *pcre.Regexp, groups []string, tmpl string) string {
	var b strings.Builder
	for i := 0; i < len(tmpl); i++ {
		if tmpl[i] != '\\' || i+1 >= len(tmpl) {
			b.WriteByte(tmpl[i])
			continue
		}
		c := tmpl[i+1]
		switch {
		case c == '\\':
			b.WriteByte('\\')
			i++
		case c >= '0' && c <= '9':
			n := int(c - '0')
			if n < len(groups) {
				b.WriteString(groups[n])
			}
			i++
		case c == 'g' && i+2 < len(tmpl) && tmpl[i+2] == '<':
			end := strings.IndexByte(tmpl[i+3:], '>')
			if end < 0 {
				b.WriteByte(tmpl[i])
				continue
			}
			name := tmpl[i+3 : i+3+end]
			if n, err := strconv.Atoi(name); err == nil {
				if n < len(groups) {
					b.WriteString(groups[n])
				}
			} else if idx := re.SubexpIndex(name); idx > 0 && idx < len(groups) {
				b.WriteString(groups[idx])
			}
			i += 3 + end
		default:
			b.WriteByte(tmpl[i])
		}
	}
	return b.String()
}

// applyLineinfilePresent mirrors real lineinfile's present(), including
// the parts a reimplementation gets wrong by being reasonable:
//
//   - WITHOUT firstmatch the loop does NOT break, so the LAST match is
//     the one edited. Measured: three lines, alpha=1/beta=2/alpha=3,
//     regexp ^alpha= -- real rewrites alpha=3 and leaves alpha=1 alone.
//     Replacing the first match edits a different line than Ansible
//     does, silently.
//   - backrefs with NO match does ABSOLUTELY NOTHING. Not an append:
//     the line cannot be built without the groups to fill it.
//   - a regexp or search_string match means insertafter/insertbefore is
//     ignored entirely; they only place a line that is not there yet.
func applyLineinfilePresent(lines []string, s lineinfileSpec) ([]string, lineinfileOutcome) {
	matchIdx := -1
	insertIdx := -1
	var groups []string
	matched := false

	if s.re != nil {
		for i, l := range lines {
			if g := s.re.FindStringSubmatch(l); g != nil {
				matchIdx, groups, matched = i, g, true
				if s.firstmatch {
					break
				}
			}
		}
	}
	if s.searchString != "" {
		for i, l := range lines {
			if strings.Contains(l, s.searchString) {
				matchIdx, matched = i, true
				if s.firstmatch {
					break
				}
			}
		}
	}
	if !matched {
	scan:
		for i, l := range lines {
			switch {
			// An exact copy of the line already in the file counts as
			// the match, which is what makes a second run report no
			// change rather than inserting a duplicate.
			case l == s.line:
				matchIdx = i
			case s.insAfterRe != nil && s.insAfterRe.MatchString(l):
				insertIdx = i + 1
				if s.firstmatch {
					break scan
				}
			case s.insBeforeRe != nil && s.insBeforeRe.MatchString(l):
				insertIdx = i
				if s.firstmatch {
					break scan
				}
			}
		}
	}

	out := append([]string{}, lines...)

	if matchIdx != -1 {
		newLine := s.line
		if s.backrefs && groups != nil {
			newLine = expandBackrefs(s.re, groups, s.line)
		}
		if out[matchIdx] == newLine {
			return out, lineinfileOutcome{}
		}
		out[matchIdx] = newLine
		return out, lineinfileOutcome{changed: true, msg: "line replaced"}
	}

	// Nothing matched. backrefs cannot build the line without groups,
	// so real does nothing at all rather than appending a template.
	if s.backrefs {
		return out, lineinfileOutcome{}
	}

	switch {
	case s.insertBefore == "BOF" || s.insertAfter == "BOF":
		out = append([]string{s.line}, out...)
	case s.insertAfter == "EOF" || insertIdx == -1:
		out = append(out, s.line)
	case insertIdx >= len(out):
		if out[len(out)-1] == s.line {
			return out, lineinfileOutcome{}
		}
		out = append(out, s.line)
	default:
		if out[insertIdx] == s.line {
			return out, lineinfileOutcome{}
		}
		out = append(out[:insertIdx], append([]string{s.line}, out[insertIdx:]...)...)
	}
	return out, lineinfileOutcome{changed: true, msg: "line added"}
}

// applyLineinfileAbsent mirrors real's absent(): the matcher is the
// regexp if given, else search_string as a SUBSTRING, else an exact
// line comparison -- and every matching line goes, not just the first.
func applyLineinfileAbsent(lines []string, s lineinfileSpec) ([]string, lineinfileOutcome) {
	var out []string
	removed := 0
	for _, l := range lines {
		var hit bool
		switch {
		case s.re != nil:
			hit = s.re.MatchString(l)
		case s.searchString != "":
			hit = strings.Contains(l, s.searchString)
		default:
			hit = l == s.line
		}
		if hit {
			removed++
			continue
		}
		out = append(out, l)
	}
	if removed == 0 {
		return out, lineinfileOutcome{}
	}
	return out, lineinfileOutcome{
		changed: true,
		msg:     fmt.Sprintf("%d line(s) removed", removed),
		removed: removed,
	}
}
