package modules

import (
	"context"
	"regexp"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleOneImageInfo implements Ansible's `one_image_info` module:
// gathers facts about OpenNebula images, via the `oneimage` CLI (see
// one_common.go's own doc comment), read-only.
//
// Args: ids ([]string, aliased id) — numeric image IDs to fetch; name
// (string) — an exact name, or (matching real one_image_info's own
// documented convention) a `~`-prefixed regex restricting which images'
// names match (case-insensitive when prefixed `~*`); with neither ids
// nor name given, every image is returned.
//
// Real one_image_info's own get_all_images_by_attributes reads the
// SAME imagepool listing regardless of which filter is active and
// applies ids/name filtering client-side — this port does the same
// (one `oneimage list -x`, filtered in Go), rather than shelling out
// per-ID.
//
// Extra field "images" is a list of the same per-image fact shape
// one_image.go's own oneImageResultWithFacts produces (see that file's
// own doc comment on the real user_id/user_name-vs-owner_id/owner_name
// naming this port matches from the actual code, not the possibly
// misleading RETURN docs) — one_image_info's own real RETURN block
// documents the identical field set per list element.
func moduleOneImageInfo(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	url := oneAuth(args)
	if res, ok := oneRequireBinary(ctx, conn, "oneimage", "one_image_info"); !ok {
		return res, nil
	}

	pool, err := oneListXML(ctx, conn, url, "oneimage")
	if err != nil {
		return Result{}, err
	}
	all := pool.children("IMAGE")

	ids := argStringList(args, "ids")
	if ids == nil {
		ids = argStringList(args, "id")
	}
	name := argString(args, "name", "")

	var nameRe *regexp.Regexp
	nameExact := ""
	if name != "" {
		if strings.HasPrefix(name, "~*") {
			re, err := regexp.Compile("(?i)" + name[2:])
			if err != nil {
				return Result{}, errArg("one_image_info: invalid name regex %q: %v", name, err)
			}
			nameRe = re
		} else if strings.HasPrefix(name, "~") {
			re, err := regexp.Compile(name[1:])
			if err != nil {
				return Result{}, errArg("one_image_info: invalid name regex %q: %v", name, err)
			}
			nameRe = re
		} else {
			nameExact = name
		}
	}

	idSet := map[string]bool{}
	for _, id := range ids {
		idSet[id] = true
	}

	images := []any{}
	for _, img := range all {
		if len(idSet) > 0 && !idSet[img.childText("ID")] {
			continue
		}
		if nameRe != nil && !nameRe.MatchString(img.childText("NAME")) {
			continue
		}
		if nameExact != "" && img.childText("NAME") != nameExact {
			continue
		}
		facts := oneImageResultWithFacts(Result{}, img)
		images = append(images, facts.Extra)
	}

	res := Ok("")
	return res.WithExtra("images", images), nil
}
