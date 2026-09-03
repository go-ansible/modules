package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleHomebrewCask implements (a subset of) Ansible's `homebrew_cask`
// module: manages Homebrew casks (GUI application packages) on macOS.
//
// Args: name (string or []string, required); state (present|installed|
// absent|removed|uninstalled|latest|upgraded, default "present");
// update_homebrew (bool, default false).
//
// This is homebrewLike (see homebrew.go) with the `--cask` flag; see
// moduleHomebrew's doc comment for the full set of simplifications
// versus real Ansible. state=head/linked/unlinked (formula-only
// concepts) are not meaningful for casks and are not accepted here —
// real homebrew_cask's own choices list also omits them.
func moduleHomebrewCask(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	state := argString(args, "state", "present")
	switch state {
	case "head", "linked", "unlinked":
		return Result{}, errArg("homebrew_cask: state %q is not valid for casks", state)
	}
	return homebrewLike(ctx, conn, args, "--cask")
}
