package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleXmlInfo implements (a subset of) Ansible's `xml_info` module: a
// read-only counterpart to xml.go, reusing its narrowed xpath grammar
// and tree-navigation helpers (xmlSplitPath/xmlFind/xmlParseDocument)
// rather than reimplementing XML parsing — this module never writes
// anything and always reports Changed=false, exactly like xml.go's own
// count/content_text/content_attributes/print_match "query" branch.
//
// Args: path (string; aliased dest/file) or xmlstring (string) — exactly
// one is required, matching real xml_info's own mutually-exclusive
// required_one_of; xpath (string, required) — "" or "/" means the
// document root itself; what (string, required: count|paths|
// content_text|content_attributes) — which piece of information to
// return; namespaces (dict, default {}) — accepted, but must be empty
// (see below); count_mode (match|xpath, default "match") — accepted, a
// no-op here (see below); strip_cdata_tags, huge_tree (bool, default
// false) — accepted, no-ops (encoding/xml has no CDATA-tag-stripping
// mode to toggle, and no libxml2-style node-size/depth safety limit to
// disable in the first place).
//
// Since this port's engine resolves at most ONE element (xml.go's own
// documented narrowing — no XPath predicates, wildcards, or functions),
// `count` is always 0 or 1, and `matches`/`content_text`/
// `content_attributes` always have at most one entry — count_mode's
// real distinction (Python len() of a full match list vs XPath's own
// count() function) has nothing to disagree about here, since there is
// only ever one possible match either way; this port accepts either
// value and always behaves the same. `namespaces` is REJECTED (a
// structural argument error, not a silent mismatch) whenever non-empty,
// for the same reason xml.go itself has no namespaces support: elements
// are matched by local name only, so a namespace-prefixed xpath segment
// cannot be honored — see xml.go's own doc comment for the full
// rationale. An xpath ending in an attribute selector ("/@attr") is
// also rejected: real xml_info's `xpath` always selects an ELEMENT
// (content_attributes already returns every attribute of the matched
// element, unlike xml.go's own single-attribute get/set model), so this
// port's xml.go-derived grammar's attribute-selector suffix has no
// meaning for xml_info and is refused rather than silently ignored.
//
// `what=paths`'s real output uses XPath position predicates for every
// ancestor level with siblings (e.g. "/business/beers/beer[2]"); since
// this port never tracks sibling position (only single-path resolution,
// no wildcards), the one path it can ever return always ends in the
// literal "[1]" — correct for the common single-match case shown in
// real xml_info's own first example, but not a faithful reproduction of
// lxml's own getpath() for a document where the matched element has
// preceding siblings, a real, disclosed narrowing.
func moduleXmlInfo(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	what, err := requireString(args, "what")
	if err != nil {
		return Result{}, err
	}
	switch what {
	case "count", "paths", "content_text", "content_attributes":
	default:
		return Result{}, errArg("xml_info: what must be one of count, paths, content_text, content_attributes, got %q", what)
	}
	xpathArg, err := requireString(args, "xpath")
	if err != nil {
		return Result{}, err
	}
	if ns, ok := args["namespaces"].(map[string]any); ok && len(ns) > 0 {
		return Result{}, errArg("xml_info: namespaces is not supported by this port (elements are matched by local name only; see xml.go's own doc comment)")
	}
	// count_mode, strip_cdata_tags, huge_tree: accepted, no effect (see
	// doc comment above).

	path := argString(args, "path", argString(args, "dest", argString(args, "file", "")))
	xmlstring := argString(args, "xmlstring", "")
	if path == "" && xmlstring == "" {
		return Result{}, errArg("xml_info: one of path (or its aliases dest/file) or xmlstring is required")
	}
	if path != "" && xmlstring != "" {
		return Result{}, errArg("xml_info: path and xmlstring are mutually exclusive")
	}

	elems, attr, err := xmlSplitPath(xpathArg)
	if err != nil {
		return Result{}, errArg("xml_info: %v", err)
	}
	if attr != "" {
		return Result{}, errArg("xml_info: this port does not support an attribute-selecting xpath (%q) for xml_info; content_attributes already returns every attribute of the matched element", xpathArg)
	}

	var data []byte
	if xmlstring != "" {
		data = []byte(xmlstring)
	} else {
		data, err = fetchIfExists(ctx, conn, path)
		if err != nil {
			return Result{}, err
		}
		if data == nil {
			return Fail(path + " does not exist"), nil
		}
	}

	_, root, err := xmlParseDocument(data)
	if err != nil {
		return Fail(fmt.Sprintf("xml_info: parsing document: %v", err)), nil
	}

	node, _, found := xmlFind(root, elems)

	res := Ok("")
	switch what {
	case "count":
		n := 0
		if found {
			n = 1
		}
		res = res.WithExtra("count", n)

	case "paths":
		var matches []any
		if found {
			matches = append(matches, xmlInfoPath(elems))
		}
		res = res.WithExtra("count", len(matches)).WithExtra("matches", matches)

	case "content_text":
		if !found {
			return Fail(fmt.Sprintf("xml_info: Xpath %s does not reference a node!", xpathArg)), nil
		}
		entries := []any{map[string]any{"tag": node.Name, "text": node.Text}}
		res = res.WithExtra("count", len(entries)).WithExtra("content_text", entries)

	case "content_attributes":
		if !found {
			return Fail(fmt.Sprintf("xml_info: Xpath %s does not reference a node!", xpathArg)), nil
		}
		attrs := map[string]any{}
		for _, a := range node.Attrs {
			attrs[a.Name.Local] = a.Value
		}
		entries := []any{map[string]any{"tag": node.Name, "attributes": attrs}}
		res = res.WithExtra("count", len(entries)).WithExtra("content_attributes", entries)
	}
	return res, nil
}

// xmlInfoPath renders elems as an absolute XPath-like path string,
// appending "[1]" to the final segment (see the doc comment above for
// why this is always "[1]" in this port).
func xmlInfoPath(elems []string) string {
	if len(elems) == 0 {
		return "/"
	}
	p := ""
	for i, e := range elems {
		p += "/" + e
		if i == len(elems)-1 {
			p += "[1]"
		}
	}
	return p
}
