package modules

import (
	"context"
	"fmt"
	"time"

	remoteexec "github.com/go-remoteexec/transport"
)

// jbossPollInterval is how long moduleJboss sleeps between polls of the
// deployment scanner's marker files. A package-level var (not a
// constant) so tests can shrink it to make the wait loop instant,
// rather than actually sleeping in a unit test.
var jbossPollInterval = 500 * time.Millisecond

// jbossMaxPollAttempts bounds the wait loop: real jboss.py polls
// forever (`while not deployed: ... time.sleep(1)`), trusting the
// deployment scanner to eventually flip a marker file one way or the
// other. This port adds a hard cap instead — an unbounded polling loop
// with no cancellation path is exactly the anti-pattern this project's
// own hard rule about "bounded wait" warns against (see
// nsapp-run-cannot-be-left doctrine) — reported as Fail (a timeout is
// an honest "the module could not confirm the outcome," not a Go
// panic-worthy bug) rather than hanging indefinitely if the scanner
// never reacts. A var (not a constant), like jbossPollInterval, so
// tests can shrink it.
var jbossMaxPollAttempts = 300

// moduleJboss implements (a subset of) Ansible's `jboss` module:
// deploys or undeploys an application to a JBoss/WildFly standalone
// server via its filesystem-based deployment scanner — read from real
// jboss.py's own is_deployed/is_undeployed/is_failed marker-file
// protocol and main()'s own polling loop (this batch's hard rule: the
// exact marker-file suffixes and polling order are only visible in the
// implementation, not EXAMPLES/OPTIONS). Real jboss is deprecated
// upstream (removed in community.general 14.0.0, in favor of the
// middleware_automation.wildfly.wildfly_app_deploy role) but still a
// real, documented module as of this port's writing.
//
// Args: deployment (string, required) — the deployment's file name
// under deploy_path, e.g. "hello.war"; src (string, required when
// state=present, ignored otherwise) — unlike real jboss (which runs ON
// the target and reads src from local disk via module.preserved_copy),
// src here is itself a TARGET-side path, matching this port's own
// "reach the target only through Connection" architecture — this port
// copies it to deploy_path+"/"+deployment with a target-side `cp -p`,
// not a control-node-to-target file transfer; deploy_path (string,
// default "/var/lib/jbossas/standalone/deployments"); state
// (present|absent, default "present").
//
// The scanner protocol (real, not invented): dropping a file named
// exactly `deployment` into deploy_path makes the scanner pick it up;
// it then creates `deployment+".deployed"` on success or
// `deployment+".failed"` on failure. Undeploying removes the
// `.deployed` marker; the scanner then creates `deployment+
// ".undeployed"`. This port polls for those markers exactly like real
// jboss does, bounded by jbossMaxPollAttempts/jbossPollInterval rather
// than polling forever (see their own doc comments).
//
// Idempotency: real jboss compares SHA1 checksums (module.sha1) of src
// and the already-deployed file to decide whether a re-deploy is
// needed. This port uses `cmp -s` instead (matching decompress.go's
// own idempotency precedent) — both src and the deployed copy are
// already target-side paths here, so a target-side byte comparison is
// both simpler and avoids needing a sha1sum/shasum binary on the
// target at all; functionally equivalent for deciding "changed".
func moduleJboss(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	deployment, err := requireString(args, "deployment")
	if err != nil {
		return Result{}, err
	}
	deployPath := argString(args, "deploy_path", "/var/lib/jbossas/standalone/deployments")
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("jboss: state must be present or absent, got %q", state)
	}
	src := argString(args, "src", "")
	if state == "present" && src == "" {
		return Result{}, errArg("jboss: src is required when state is present")
	}

	exists, err := pathExists(ctx, conn, deployPath)
	if err != nil {
		return Result{}, err
	}
	if !exists {
		return Fail("jboss: deploy_path does not exist."), nil
	}

	if state == "present" {
		srcExists, err := pathExists(ctx, conn, src)
		if err != nil {
			return Result{}, err
		}
		if !srcExists {
			return Fail(fmt.Sprintf("jboss: Source file %s does not exist.", src)), nil
		}
	}

	deployedMarker := deployPath + "/" + deployment + ".deployed"
	failedMarker := deployPath + "/" + deployment + ".failed"
	undeployedMarker := deployPath + "/" + deployment + ".undeployed"
	deployedFile := deployPath + "/" + deployment

	deployed, err := pathExists(ctx, conn, deployedMarker)
	if err != nil {
		return Result{}, err
	}

	if state == "present" {
		if !deployed {
			if failed, err := pathExists(ctx, conn, failedMarker); err != nil {
				return Result{}, err
			} else if failed {
				if _, err := run(ctx, conn, "rm -f "+shellQuote(failedMarker)); err != nil {
					return Result{}, err
				}
			}
			if _, err := run(ctx, conn, "cp -p "+shellQuote(src)+" "+shellQuote(deployedFile)); err != nil {
				return Result{}, err
			}
			if failMsg, err := jbossWaitDeployed(ctx, conn, deployedMarker, failedMarker, deployment); err != nil {
				return Result{}, err
			} else if failMsg != "" {
				return Fail(failMsg), nil
			}
			return Changed(""), nil
		}

		same, err := jbossSame(ctx, conn, src, deployedFile)
		if err != nil {
			return Result{}, err
		}
		if same {
			return Ok(""), nil
		}
		if _, err := run(ctx, conn, "rm -f "+shellQuote(deployedMarker)); err != nil {
			return Result{}, err
		}
		if _, err := run(ctx, conn, "cp -p "+shellQuote(src)+" "+shellQuote(deployedFile)); err != nil {
			return Result{}, err
		}
		if failMsg, err := jbossWaitDeployed(ctx, conn, deployedMarker, failedMarker, deployment); err != nil {
			return Result{}, err
		} else if failMsg != "" {
			return Fail(failMsg), nil
		}
		return Changed(""), nil
	}

	// state == "absent"
	if !deployed {
		return Ok(""), nil
	}
	if _, err := run(ctx, conn, "rm -f "+shellQuote(deployedMarker)); err != nil {
		return Result{}, err
	}
	for i := 0; ; i++ {
		undeployed, err := pathExists(ctx, conn, undeployedMarker)
		if err != nil {
			return Result{}, err
		}
		if undeployed {
			break
		}
		if failed, err := pathExists(ctx, conn, failedMarker); err != nil {
			return Result{}, err
		} else if failed {
			return Fail(fmt.Sprintf("jboss: Undeploying %s failed.", deployment)), nil
		}
		if i >= jbossMaxPollAttempts {
			return Fail(fmt.Sprintf("jboss: timed out waiting for %s to be undeployed by the scanner.", deployment)), nil
		}
		select {
		case <-ctx.Done():
			return Result{}, ctx.Err()
		case <-time.After(jbossPollInterval):
		}
	}
	return Changed(""), nil
}

// jbossSame reports whether src and dest already have identical
// content on the target, via `cmp -s` — see moduleJboss's own doc
// comment for why this port uses that instead of real jboss's own
// SHA1-checksum comparison.
func jbossSame(ctx context.Context, conn remoteexec.Connection, src, dest string) (bool, error) {
	res, err := runStatus(ctx, conn, "cmp -s "+shellQuote(src)+" "+shellQuote(dest))
	if err != nil {
		return false, err
	}
	return res.RC == 0, nil
}

// jbossWaitDeployed polls deployedMarker/failedMarker after a copy,
// bounded by jbossMaxPollAttempts/jbossPollInterval (see their own doc
// comments) — matching real main()'s own `while not deployed: ...`
// loop, but never running forever.
func jbossWaitDeployed(ctx context.Context, conn remoteexec.Connection, deployedMarker, failedMarker, deployment string) (failMsg string, err error) {
	for i := 0; ; i++ {
		deployed, err := pathExists(ctx, conn, deployedMarker)
		if err != nil {
			return "", err
		}
		if deployed {
			return "", nil
		}
		if failed, err := pathExists(ctx, conn, failedMarker); err != nil {
			return "", err
		} else if failed {
			return fmt.Sprintf("jboss: Deploying %s failed.", deployment), nil
		}
		if i >= jbossMaxPollAttempts {
			return fmt.Sprintf("jboss: timed out waiting for %s to be deployed by the scanner.", deployment), nil
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(jbossPollInterval):
		}
	}
}
