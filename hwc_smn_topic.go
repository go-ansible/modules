package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleHwcSmnTopic implements Ansible's `hwc_smn_topic`
// (community.general) module: creates or deletes a Huawei Cloud Simple
// Message Notification (SMN) topic — see hwc_common.go's own doc
// comment for the KooCLI substitution shared by every hwc_* module in
// this batch. Operation IDs (CreateTopic/ShowTopic/DeleteTopic/
// ListTopics, KooCLI service code "SMN") are the ones this batch's own
// research most directly confirmed: Huawei's own published API
// reference documents CreateTopic (POST .../notifications/topics) and
// DeleteTopic by name; ShowTopic/ListTopics follow the same confirmed
// PascalCase(Verb+Resource) convention hwc_common.go's own doc comment
// describes, applied to the "topics" resource real hwc_smn_topic.py's
// own REST path uses.
//
// Args: name (required); display_name (optional); id (accepted for
// argument-shape compatibility — but see below); state
// (present|absent, default present). This module has no `region`
// argument in its own real argument_spec, so none is accepted here
// either.
//
// Deviation — identity: a real Huawei SMN topic's own stable
// identifier is its topic_urn (an ARN-like string), not a bare
// numeric/UUID "id" the way most of this batch's other hwc_* resources
// use — real hwc_smn_topic.py's own selection logic is actually by
// `name` (SMN topic names are unique per project), which this port
// mirrors directly: lookup is always by ListTopics filtered
// client-side by name, and the id argument (present on this module
// only for cross-module argument-shape consistency within this batch)
// is NOT used for lookup here — a documented, deliberate deviation
// from hwc_common.go's own general id-takes-precedence convention,
// because unlike every other resource in this batch, real
// hwc_smn_topic.py's own module has no concept of looking a topic up
// by a plain id at all.
//
// Extra["id"]/Extra["topic"]: Extra["id"] is the topic's own
// topic_urn (matching real hwc_smn_topic.py's own returned `id`
// field, which real Huawei SMN documents as being the topic_urn);
// Extra["topic"] is the topic's raw JSON attributes as KooCLI itself
// returned them. Both present whenever the topic now exists.
func moduleHwcSmnTopic(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := hcloudRequireBinary(ctx, conn, "hwc_smn_topic"); !ok {
		return res, nil
	}
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("hwc_smn_topic: state must be one of present, absent, got %q", state)
	}

	var listResp map[string]any
	lres, err := hcloudRunJSON(ctx, conn, "SMN", "ListTopics", map[string]string{"name": name}, &listResp)
	if err != nil {
		return Result{}, err
	}
	if lres.RC != 0 {
		return hcloudFail("hwc_smn_topic", "listing topics", lres), nil
	}
	items := hcloudListArray(listResp)
	match, found, ambiguous := hcloudFindOne(items, map[string]string{"name": name})
	if ambiguous {
		return Fail("hwc_smn_topic: more than one topic matches name=" + name + "; execution aborted"), nil
	}

	if state == "absent" {
		if !found {
			return Ok("hwc_smn_topic: " + name + " already absent"), nil
		}
		urn := fmt.Sprint(match["topic_urn"])
		dres, err := hcloudRun(ctx, conn, "SMN", "DeleteTopic", map[string]string{"topic_urn": urn})
		if err != nil {
			return Result{}, err
		}
		if dres.RC != 0 {
			return hcloudFail("hwc_smn_topic", "deleting topic "+name, dres), nil
		}
		return Changed("hwc_smn_topic: "+name+" deleted").WithExtra("id", urn), nil
	}

	if found {
		return Ok("hwc_smn_topic: "+name+" already present").
			WithExtra("id", fmt.Sprint(match["topic_urn"])).WithExtra("topic", match), nil
	}

	cparams := map[string]string{"name": name}
	if v := argString(args, "display_name", ""); v != "" {
		cparams["display_name"] = v
	}

	var created map[string]any
	cres, err := hcloudRunJSON(ctx, conn, "SMN", "CreateTopic", cparams, &created)
	if err != nil {
		return Result{}, err
	}
	if cres.RC != 0 {
		return hcloudFail("hwc_smn_topic", "creating topic "+name, cres), nil
	}
	r := Changed("hwc_smn_topic: " + name + " created")
	if urn, ok := created["topic_urn"]; ok {
		r = r.WithExtra("id", fmt.Sprint(urn)).WithExtra("topic", created)
	}
	return r, nil
}
