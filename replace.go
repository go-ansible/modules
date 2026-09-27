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
		// Real's own wording, space before the bang included, and its
		// own rc. Measured: msg "Path <p> does not exist !", rc 257.
		return Fail(fmt.Sprintf("Path %s does not exist !", path)).
			WithExtra("rc", 257), nil
	}

	// Real reports HOW MANY replacements it made, so the count is what
	// it substitutes with -- not a plain ReplaceAll.
	count := len(re.FindAllString(string(current), -1))
	updated := re.ReplaceAllString(string(current), replacement)
	if updated == string(current) {
		// Measured: an unchanged replace reports an EMPTY msg, and rc 0
		// all the same. Real's keys here are changed, failed, msg, rc.
		return Ok("").WithExtra("rc", 0), nil
	}
	// Every "unchanged" case has already returned above, so reaching here
	// means the file WOULD be rewritten. Check mode reports that and
	// stops short of the one write.
	// "1 replacements made" -- real does not singularise, and matching
	// it means not singularising either.
	res := Changed(fmt.Sprintf("%d replacements made", count)).WithExtra("rc", 0)
	if InDiffMode(args) {
		res = res.WithDiff(ContentDiff(path, path, current, []byte(updated)))
	}
	if InCheckMode(args) {
		return res, nil
	}
	if err := writeRemote(ctx, conn, path, []byte(updated)); err != nil {
		return Result{}, err
	}
	return res, nil
}
