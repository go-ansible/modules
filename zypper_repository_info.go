package modules

import (
	"context"
	"encoding/xml"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleZypperRepositoryInfo implements Ansible's `zypper_repository_info`
// module: lists Zypper repositories on SUSE/openSUSE. Read-only —
// always reports unchanged.
//
// Args: none.
//
// Returns Extra["repodatalist"] — a list of maps, one per configured
// repository, each with string fields alias/name/priority/enabled/
// autorefresh/gpgcheck/url, matching real zypper_repository_info's own
// RETURN VALUES exactly (both this port and the real module get these
// straight from `zypper --xmlout repos`'s per-repo attributes/element
// without any further type conversion, so e.g. enabled is the literal
// string "1"/"0" zypper prints, not a Go bool).
func moduleZypperRepositoryInfo(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	repos, err := queryZypperRepos(ctx, conn)
	if err != nil {
		return Result{}, err
	}
	return Ok("").WithExtra("repodatalist", zypperRepoListAsMaps(repos)), nil
}

// zypperRepoXML mirrors one <repo> element of `zypper --xmlout repos`'s
// output, matching real zypper_repository.py/zypper_repository_info.py's
// own REPO_OPTS = [alias, name, priority, enabled, autorefresh, gpgcheck]
// plus the <url> child element.
type zypperRepoXML struct {
	Alias       string `xml:"alias,attr"`
	Name        string `xml:"name,attr"`
	Priority    string `xml:"priority,attr"`
	Enabled     string `xml:"enabled,attr"`
	Autorefresh string `xml:"autorefresh,attr"`
	GpgCheck    string `xml:"gpgcheck,attr"`
	URL         string `xml:"url"`
}

type zypperRepoListXML struct {
	Repos []zypperRepoXML `xml:"repo-list>repo"`
}

// queryZypperRepos runs `zypper --quiet --non-interactive --xmlout repos`
// and parses its output. Exit code 6 (ZYPPER_EXIT_NO_REPOS) is not an
// error — it means no repositories are configured, matching real
// zypper_repository.py/zypper_repository_info.py's own handling of that
// specific exit code.
func queryZypperRepos(ctx context.Context, conn remoteexec.Connection) ([]zypperRepoXML, error) {
	res, err := runStatus(ctx, conn, "zypper --quiet --non-interactive --xmlout repos")
	if err != nil {
		return nil, err
	}
	switch res.RC {
	case 0:
	case 6:
		return nil, nil
	default:
		return nil, fmt.Errorf("zypper --xmlout repos: exit %d: %s", res.RC, res.Stderr)
	}
	var parsed zypperRepoListXML
	if err := xml.Unmarshal([]byte(res.Stdout), &parsed); err != nil {
		return nil, fmt.Errorf("parsing zypper --xmlout repos output: %w", err)
	}
	return parsed.Repos, nil
}

func zypperRepoListAsMaps(repos []zypperRepoXML) []map[string]any {
	out := make([]map[string]any, 0, len(repos))
	for _, r := range repos {
		out = append(out, map[string]any{
			"alias":       r.Alias,
			"name":        r.Name,
			"priority":    r.Priority,
			"enabled":     r.Enabled,
			"autorefresh": r.Autorefresh,
			"gpgcheck":    r.GpgCheck,
			"url":         r.URL,
		})
	}
	return out
}
