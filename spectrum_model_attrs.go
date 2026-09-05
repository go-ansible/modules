package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleSpectrumModelAttrs implements Ansible's `spectrum_model_attrs`
// (community.general) module: enforces a set of attribute values on an
// existing CA Spectrum model (identified by name+type), via Broadcom's
// own official vnmsh CLI — see spectrum_common.go's own doc comment
// for the verified architecture mismatch (vnmsh is local to the
// SpectroSERVER, unlike real spectrum_model_attrs.py's own remote
// OneClick REST calls) and the `./current`/`./update` command syntax
// this port relies on.
//
// # Model lookup: name+type, matching real find_model_by_name_type()
//
// Real spectrum_model_attrs.py's own find_model_by_name_type() searches
// by Model_Name (hex 0x1006e) AND Modeltype_Name (hex 0x10000) together
// — vnmsh's own `seek` command (spectrum_common.go's own spectrumSeek)
// only takes ONE attr=/val= filter pair per call, with no documented
// AND-of-two-attributes shape this port could verify. This port
// therefore seeks by Model_Name first (the more selective of the two
// in practice) and, when found, does not independently re-verify type
// — a real, honestly-noted gap: a caller with two same-named models of
// DIFFERENT types (an edge case real spectrum_model_attrs.py itself
// treats as an outright error, "More than one model found") could get
// the wrong one from this port. This is documented rather than
// silently accepted, matching this project's own "if real behavior
// can't be replicated through this port's architecture, document that
// honestly" rule — a caller relying on type-disambiguation for
// same-named models across types should not use this port's version
// yet.
//
// # Attribute names, matching real spectrum_model_attrs.py's own map
//
// This port reuses real spectrum_model_attrs.py's own attr_map
// verbatim (App_Manufacturer/CollectionsModelNameString/Condition/
// Criticality/DeviceType/isManaged/Model_Class/Model_Handle/Model_Name/
// Modeltype_Handle/Modeltype_Name/Network_Address/Notes/
// ServiceDesk_Asset_ID/TopologyModelNameString/sysDescr/sysName/
// Vendor_Name/Description → their own documented hex IDs) — a name not
// in that map is passed straight through as-is, matching real
// attr_id()'s own `or name` fallback ("Hex IDs are the direct
// identifiers in Spectrum and always work").
//
// Args: url, url_username (alias username), url_password (alias
// password) — accepted but not used to authenticate, see
// spectrum_common.go's own doc comment; name (required, model name);
// type (required, model type name); attributes (required, list of
// {name, value}) — this port has no read-then-compare step (unlike
// real ensure_model_attrs(), which only PUTs an attribute whose
// current value actually differs) since vnmsh's own `./show`/
// attribute-read output shape could not be verified against a live
// system either (the same honestly-bounded gap spectrum_common.go's
// own doc comment already flags for `seek`) — this port therefore
// always issues `./current`+`./update` for every attribute given,
// every run, and always reports Changed=true on success. A caller
// relying on real spectrum_model_attrs.py's own no-op-when-unchanged
// behavior will see a behavioral difference here.
func moduleSpectrumModelAttrs(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	const mod = "spectrum_model_attrs"
	if _, err := requireString(args, "url"); err != nil {
		return Result{}, err
	}
	username := argString(args, "url_username", argString(args, "username", ""))
	if username == "" {
		return Result{}, errArg("%s: missing required argument: url_username (or username)", mod)
	}
	password := argString(args, "url_password", argString(args, "password", ""))
	if password == "" {
		return Result{}, errArg("%s: missing required argument: url_password (or password)", mod)
	}
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	modelType, err := requireString(args, "type")
	if err != nil {
		return Result{}, err
	}
	rawAttrs, ok := args["attributes"]
	if !ok {
		return Result{}, errArg("%s: missing required argument: attributes", mod)
	}
	attrs, err := spectrumParseAttributes(rawAttrs)
	if err != nil {
		return Result{}, err
	}
	if len(attrs) == 0 {
		return Result{}, errArg("%s: attributes must be a non-empty list", mod)
	}

	if res, ok := spectrumRequireBinary(ctx, conn, mod); !ok {
		return res, nil
	}

	handle, found, err := spectrumSeek(ctx, conn, spectrumAttrID("Model_Name"), name, "")
	if err != nil {
		return Result{}, err
	}
	if !found {
		return Fail(mod + ": no model found matching name `" + name + "' (type `" + modelType + "' not independently " +
			"verified — see moduleSpectrumModelAttrs's own doc comment)"), nil
	}

	changedAttrs := map[string]any{}
	for _, a := range attrs {
		attrID := spectrumAttrID(a.Name)
		res, err := spectrumUpdateAttr(ctx, conn, handle, attrID, a.Value)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail(mod+": failed to set attribute `"+a.Name+"' on model "+handle+": "+
				spectrumErrMsg(res)).WithExtra("changed_attrs", changedAttrs), nil
		}
		changedAttrs[a.Name] = a.Value
	}

	return Changed("Success").WithExtra("changed_attrs", changedAttrs), nil
}

type spectrumAttrPair struct {
	Name  string
	Value string
}

func spectrumParseAttributes(v any) ([]spectrumAttrPair, error) {
	list, ok := v.([]any)
	if !ok {
		return nil, errArg("spectrum_model_attrs: attributes must be a list")
	}
	out := make([]spectrumAttrPair, 0, len(list))
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, errArg("spectrum_model_attrs: each attributes entry must be a mapping with name/value")
		}
		name := argString(m, "name", "")
		if name == "" {
			return nil, errArg("spectrum_model_attrs: attributes entry missing required 'name'")
		}
		value := argString(m, "value", "")
		out = append(out, spectrumAttrPair{Name: name, Value: value})
	}
	return out, nil
}

// spectrumAttrMap mirrors real spectrum_model_attrs.py's own attr_map
// verbatim — see moduleSpectrumModelAttrs's own doc comment.
var spectrumAttrMap = map[string]string{
	"App_Manufacturer":           "0x230683",
	"CollectionsModelNameString": "0x12adb",
	"Condition":                  "0x1000a",
	"Criticality":                "0x1290c",
	"DeviceType":                 "0x23000e",
	"isManaged":                  "0x1295d",
	"Model_Class":                "0x11ee8",
	"Model_Handle":               "0x129fa",
	"Model_Name":                 "0x1006e",
	"Modeltype_Handle":           "0x10001",
	"Modeltype_Name":             "0x10000",
	"Network_Address":            "0x12d7f",
	"Notes":                      "0x11564",
	"ServiceDesk_Asset_ID":       "0x12db9",
	"TopologyModelNameString":    "0x129e7",
	"sysDescr":                   "0x10052",
	"sysName":                    "0x10b5b",
	"Vendor_Name":                "0x11570",
	"Description":                "0x230017",
}

// spectrumAttrID returns name's own hex ID, or name itself unchanged if
// not in the map — matching real attr_id()'s own `or name` fallback.
func spectrumAttrID(name string) string {
	if id, ok := spectrumAttrMap[name]; ok {
		return id
	}
	return name
}
