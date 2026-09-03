package modules

import (
	"context"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleMavenArtifact implements (a subset of) Ansible's
// `maven_artifact` module: downloads an artifact identified by Maven
// coordinates from a Maven repository to a path on the target.
//
// Args: group_id, artifact_id, version (all strings, required) — the
// Maven coordinates; classifier (string, optional); extension (string,
// default "jar"); repository_url (string, default
// "https://repo1.maven.org/maven2"); dest (string, required, remote
// path); state (present|absent, default "present"); mode (octal
// string, optional).
//
// This composes the artifact's download URL using the standard Maven2
// repository layout — <repository_url>/<group_id with dots turned into
// slashes>/<artifact_id>/<version>/<artifact_id>-<version>[-<classifier>].
// <extension> — then downloads it exactly like get_url.go's own
// moduleGetURL does (reusing its getURLCmd curl-with-wget-fallback
// composition), since a Maven repository is, mechanically, just an
// HTTP(S) file server laid out by a fixed convention; no separate
// download primitive is needed once the URL is built.
//
// Simplifications vs real maven_artifact, all deliberately left
// unimplemented:
//
//   - No `version_by_spec` or `version: latest` resolution. Both
//     require fetching and parsing the repository's maven-metadata.xml
//     to pick a concrete version, which this port does not do — version
//     must be a literal string.
//   - No checksum verification (`verify_checksum`/`checksum_alg`).
//     Like get_url.go, idempotency here is existence-only:
//     state=present skips the download whenever dest already exists on
//     the target, so a changed artifact republished under the same
//     coordinates is not detected. This mirrors get_url.go's own
//     documented narrowing, applied here for the same reason (no cheap
//     way to compare a remote hash from a shell probe without adding a
//     second network round trip this port chooses not to make
//     unconditional).
//   - No authentication (`username`/`password`/`client_cert`/
//     `client_key`/`force_basic_auth`) and no `s3://` or `file://`
//     repository schemes — only plain HTTP(S), via the same curl/wget
//     composition get_url.go uses.
//   - No owner/group/selinux context, `attributes`, `directory_mode`,
//     `keep_name`, `headers`, `timeout`, or `unsafe_writes` support.
//
// state=absent removes dest if present. Real maven_artifact's own doc
// text does not spell out what `absent` does beyond the state choice
// existing; removing the previously-downloaded artifact is the natural
// reading and keeps this module's present/absent symmetric with the
// rest of this package (e.g. dnf.go, apt.go).
func moduleMavenArtifact(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	groupID, err := requireString(args, "group_id")
	if err != nil {
		return Result{}, err
	}
	artifactID, err := requireString(args, "artifact_id")
	if err != nil {
		return Result{}, err
	}
	version, err := requireString(args, "version")
	if err != nil {
		return Result{}, err
	}
	dest, err := requireString(args, "dest")
	if err != nil {
		return Result{}, err
	}
	classifier := argString(args, "classifier", "")
	extension := argString(args, "extension", "jar")
	repositoryURL := argString(args, "repository_url", "https://repo1.maven.org/maven2")
	state := argString(args, "state", "present")
	mode, err := argMode(args, "mode")
	if err != nil {
		return Result{}, err
	}

	url := mavenArtifactURL(repositoryURL, groupID, artifactID, version, classifier, extension)

	switch state {
	case "absent":
		exists, err := pathExists(ctx, conn, dest)
		if err != nil {
			return Result{}, err
		}
		if !exists {
			return Ok(dest).WithExtra("dest", dest), nil
		}
		if _, err := run(ctx, conn, "rm -f "+shellQuote(dest)); err != nil {
			return Result{}, err
		}
		return Changed(dest).WithExtra("dest", dest), nil

	case "present":
		exists, err := pathExists(ctx, conn, dest)
		if err != nil {
			return Result{}, err
		}

		changed := false
		if !exists {
			res, err := runStatus(ctx, conn, getURLCmd(dest, url))
			if err != nil {
				return Result{}, err
			}
			if res.RC != 0 {
				return Fail(fmt.Sprintf("maven_artifact: downloading %s: %s", url, strings.TrimSpace(res.Stderr))), nil
			}
			changed = true
		}

		if mode != nil {
			info, err := statPath(ctx, conn, dest)
			if err != nil {
				return Result{}, err
			}
			if info == nil || info.mode != *mode {
				if _, err := run(ctx, conn, fmt.Sprintf("chmod %04o %s", *mode, shellQuote(dest))); err != nil {
					return Result{}, err
				}
				changed = true
			}
		}

		r := Ok(dest)
		if changed {
			r = Changed(dest)
		}
		return r.WithExtra("dest", dest).WithExtra("url", url), nil

	default:
		return Result{}, errArg("maven_artifact: state must be present or absent, got %q", state)
	}
}

// mavenArtifactURL composes the download URL for a Maven artifact under
// the standard Maven2 repository layout: <repositoryURL>/<groupID with
// dots replaced by slashes>/<artifactID>/<version>/<artifactID>-
// <version>[-<classifier>].<extension>.
func mavenArtifactURL(repositoryURL, groupID, artifactID, version, classifier, extension string) string {
	groupPath := strings.ReplaceAll(groupID, ".", "/")
	filename := artifactID + "-" + version
	if classifier != "" {
		filename += "-" + classifier
	}
	filename += "." + extension
	return strings.TrimRight(repositoryURL, "/") + "/" + groupPath + "/" + artifactID + "/" + version + "/" + filename
}
