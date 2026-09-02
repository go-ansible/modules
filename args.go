package modules

import (
	"fmt"
	"strconv"
)

// argError reports a malformed or missing module argument — the Go
// error a Func returns when it cannot even attempt to run, as opposed
// to a Result{Failed:true} for a well-formed argument set that fails at
// runtime (a file that doesn't exist, a command that exits non-zero).
type argError struct {
	msg string
}

func (e *argError) Error() string { return e.msg }

func errArg(format string, a ...any) error {
	return &argError{msg: fmt.Sprintf(format, a...)}
}

func argString(args map[string]any, key, def string) string {
	if v, ok := args[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
		return fmt.Sprint(v)
	}
	return def
}

func requireString(args map[string]any, key string) (string, error) {
	v, ok := args[key]
	if !ok {
		return "", errArg("missing required argument: %s", key)
	}
	s, ok := v.(string)
	if !ok {
		return "", errArg("argument %s must be a string, got %T", key, v)
	}
	if s == "" {
		return "", errArg("argument %s must not be empty", key)
	}
	return s, nil
}

func argBool(args map[string]any, key string, def bool) bool {
	v, ok := args[key]
	if !ok {
		return def
	}
	switch b := v.(type) {
	case bool:
		return b
	case string:
		parsed, err := strconv.ParseBool(b)
		if err == nil {
			return parsed
		}
	}
	return def
}

func argInt(args map[string]any, key string, def int) int {
	v, ok := args[key]
	if !ok {
		return def
	}
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case string:
		parsed, err := strconv.Atoi(n)
		if err == nil {
			return parsed
		}
	}
	return def
}

func argStringList(args map[string]any, key string) []string {
	v, ok := args[key]
	if !ok {
		return nil
	}
	switch list := v.(type) {
	case []string:
		return list
	case []any:
		out := make([]string, 0, len(list))
		for _, item := range list {
			out = append(out, fmt.Sprint(item))
		}
		return out
	case string:
		return []string{list}
	}
	return nil
}

// argMode returns the "mode" argument (a permission string like "0644"
// or "u+x") as a *uint32 file-mode bitmask, or nil if unset or not a
// plain octal string (symbolic modes like "u+x" are not parsed by this
// package — a caller wanting that composes it against the current mode
// itself; the common case is a literal octal mode).
func argMode(args map[string]any, key string) (*uint32, error) {
	v, ok := args[key]
	if !ok {
		return nil, nil
	}
	s, ok := v.(string)
	if !ok {
		s = fmt.Sprint(v)
	}
	n, err := strconv.ParseUint(s, 8, 32)
	if err != nil {
		return nil, errArg("argument %s: invalid octal mode %q: %v", key, s, err)
	}
	m := uint32(n)
	return &m, nil
}
