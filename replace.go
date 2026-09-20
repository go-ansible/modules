package modules

import (
	"context"
	"fmt"

	pcre "github.com/go-regexp/engine"
	remoteexec "github.com/go-remoteexec/transport"
)

// moduleReplace implements Ansible's `replace` module: replaces every
// match of a regexp in a file's content with a replacement string
// (Go's regexp $1/${name} backreference syntax).
//
// Args: path (string, required); regexp (string, required); replace
// (string, default "").
func moduleReplace(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	path, err := requireString(args, "path")
	if err != nil {
		return Result{}, err
	}
	pattern, err := requireString(args, "regexp")
	if err != nil {
		return Result{}, err
	}
	replacement := argString(args, "replace", "")

	re, err := pcre.Compile(pattern)
	if err != nil {
		return Result{}, errArg("replace: invalid regexp: %v", err)
	}

	current, err := fetchIfExists(ctx, conn, path)
	if err != nil {
		return Result{}, err
	}
	if current == nil {
		return Fail(fmt.Sprintf("%s does not exist", path)), nil
	}

	updated := re.ReplaceAllString(string(current), replacement)
	if updated == string(current) {
		return Ok(path + " unchanged"), nil
	}
	// Every "unchanged" case has already returned above, so reaching here
	// means the file WOULD be rewritten. Check mode reports that and
	// stops short of the one write.
	res := Changed(path)
	if InDiffMode(args) {
		res = res.WithDiff(ContentDiff(path, current, []byte(updated)))
	}
	if InCheckMode(args) {
		return res, nil
	}
	if err := writeRemote(ctx, conn, path, []byte(updated)); err != nil {
		return Result{}, err
	}
	return res, nil
}
