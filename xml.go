package modules

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleXml implements a DELIBERATELY NARROW subset of Ansible's `xml`
// module. Real xml is a general XPath-based CRUD tool built on lxml
// (libxml2); Go's standard library has no XPath engine, and this
// package's stated preference (see this batch's other modules) is a
// narrower but honestly-scoped module over a broken general one. So
// rather than faking predicates/wildcards/functions, this port only
// resolves a SIMPLE, absolute, slash-separated element path — no `[...]`
// predicates, no `*` wildcards, no XPath functions, no relative paths —
// optionally ending in `/@attrname` to select an attribute, matching the
// two forms shown in real xml's own examples
// (`/business/rating/@subjective` and the separate `attribute:` arg).
// Anything past that (`/config/element[@name='test1']`, `//*`, etc.) is
// rejected with a clear error rather than silently mismatching.
//
// Args: path (string, required; aliased from dest/file); xpath (string)
// — "" or "/" means the document's root element itself; attribute
// (string, optional) — alternative to a trailing "/@attr" in xpath;
// value (string, optional raw in real xml — accepted as a string here);
// state (present|absent, default "present"); count (bool, default
// false); content (attribute|text, optional) — get instead of set;
// print_match (bool, default false); create_if_missing (bool, default
// true); backup (bool, default false).
//
// Since this port resolves at most one element per path (no wildcards),
// `count` is always 0 or 1 and `matches` always has at most one entry —
// real xml's own multi-match XPath semantics don't apply here, which is
// the essence of the narrowing: this module answers "does this one
// simple path exist, and what's there" honestly, rather than pretending
// to support arbitrary XPath and quietly matching the wrong (or no)
// nodes.
//
// NOT supported (a real gap vs real xml, not a silent one):
// `namespaces` (elements are matched by local name only, ignoring any
// xmlns mapping — a namespace-prefixed xpath segment like `x:foo` is
// treated as the literal string "x:foo" would need to also appear as
// the tag's own local name, which it never will for a real
// namespaced document); `add_children`, `set_children`, `insertafter`,
// `insertbefore`, `input_type`; `xmlstring` (path is required); `
// pretty_print`, `huge_tree`, `strip_cdata_tags`.
//
// Round-trip fidelity: this port parses into a plain Go struct tree via
// encoding/xml and re-serializes it — comments, processing instructions
// (other than a leading `<?xml ...?>` declaration, which IS preserved),
// DOCTYPE declarations, whitespace-only text nodes between elements, and
// original attribute/element ordering-on-rewrite subtleties are NOT
// preserved the way a real libxml2-based tool preserves them. Output is
// not re-indented to match the input's formatting. This is a real
// fidelity gap versus real xml, called out here rather than claimed
// away.
func moduleXml(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	path, err := xmlRequirePath(args)
	if err != nil {
		return Result{}, err
	}
	xpathArg := argString(args, "xpath", "")
	elems, attrFromPath, err := xmlSplitPath(xpathArg)
	if err != nil {
		return Result{}, errArg("xml: %v", err)
	}
	attribute := argString(args, "attribute", "")
	if attribute == "" {
		attribute = attrFromPath
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("xml: state must be present or absent, got %q", state)
	}
	count := argBool(args, "count", false)
	content := argString(args, "content", "")
	printMatch := argBool(args, "print_match", false)
	createIfMissing := argBool(args, "create_if_missing", true)
	backup := argBool(args, "backup", false)

	valueStr := ""
	hasValue := false
	if v, ok := args["value"]; ok {
		hasValue = true
		if s, ok2 := v.(string); ok2 {
			valueStr = s
		} else {
			valueStr = fmt.Sprint(v)
		}
	}

	data, err := fetchIfExists(ctx, conn, path)
	if err != nil {
		return Result{}, err
	}
	if data == nil {
		return Fail(path + " does not exist (real xml requires the file to exist ahead of time)"), nil
	}
	decl, root, err := xmlParseDocument(data)
	if err != nil {
		return Fail(fmt.Sprintf("xml: parsing %s: %v", path, err)), nil
	}

	node, parent, found := xmlFind(root, elems)
	actions := map[string]any{"xpath": xpathArg, "state": state}

	if count || content != "" || printMatch {
		n := 0
		var matches []any
		if found {
			n = 1
			matches = append(matches, xmlMatchValue(node, attribute))
		}
		r := Ok(path).WithExtra("actions", actions)
		if count {
			r = r.WithExtra("count", n)
		}
		if content != "" || printMatch {
			r = r.WithExtra("matches", matches)
		}
		return r, nil
	}

	changed := false
	switch state {
	case "absent":
		if !found {
			return Ok(path+" unchanged").WithExtra("actions", actions), nil
		}
		if attribute != "" {
			if _, ok := xmlGetAttr(node, attribute); !ok {
				return Ok(path+" unchanged").WithExtra("actions", actions), nil
			}
			xmlRemoveAttr(node, attribute)
			changed = true
		} else {
			if parent == nil {
				return Fail("xml: cannot remove the document root element"), nil
			}
			xmlRemoveChild(parent, node)
			changed = true
		}

	case "present":
		if !found {
			if !createIfMissing {
				return Ok(path+" unchanged (no match, create_if_missing is false)").WithExtra("actions", actions), nil
			}
			node, _, _, err = xmlEnsure(root, elems)
			if err != nil {
				return Result{}, errArg("xml: %v", err)
			}
			changed = true
		}
		if attribute != "" {
			cur, ok := xmlGetAttr(node, attribute)
			if !ok || cur != valueStr {
				xmlSetAttr(node, attribute, valueStr)
				changed = true
			}
		} else if hasValue {
			if node.Text != valueStr {
				node.Text = valueStr
				changed = true
			}
		}
		// Else: the element is now present with no value given — real
		// xml's own documented default ("Elements default to no value
		// (but present)").
	}

	if !changed {
		return Ok(path+" unchanged").WithExtra("actions", actions), nil
	}

	out, err := xmlSerializeDocument(decl, root)
	if err != nil {
		return Result{}, fmt.Errorf("xml: serializing %s: %w", path, err)
	}
	if backup {
		if _, err := run(ctx, conn, "cp "+shellQuote(path)+" "+shellQuote(path)+".$(date +%Y%m%d%H%M%S) 2>/dev/null"); err != nil {
			return Result{}, err
		}
	}
	if err := writeRemote(ctx, conn, path, out); err != nil {
		return Result{}, err
	}
	return Changed(path).WithExtra("actions", actions), nil
}

func xmlRequirePath(args map[string]any) (string, error) {
	for _, key := range []string{"path", "dest", "file"} {
		if s, ok := args[key].(string); ok && s != "" {
			return s, nil
		}
	}
	return "", errArg("xml: path (or its aliases dest/file) is required")
}

// xmlSplitPath parses this port's narrowed xpath grammar: an absolute,
// slash-separated list of simple element names, optionally ending in
// "@attrname" to select an attribute. "" or "/" means the document root
// itself (no element segments).
func xmlSplitPath(xpathArg string) (elems []string, attr string, err error) {
	p := strings.TrimSpace(xpathArg)
	if p == "" || p == "/" {
		return nil, "", nil
	}
	if !strings.HasPrefix(p, "/") {
		return nil, "", fmt.Errorf("xpath %q must be an absolute path starting with \"/\"; this port does not support relative paths", xpathArg)
	}
	segs := strings.Split(strings.TrimPrefix(p, "/"), "/")
	for i, s := range segs {
		if s == "" {
			return nil, "", fmt.Errorf("xpath %q has an empty path segment", xpathArg)
		}
		if strings.HasPrefix(s, "@") {
			if i != len(segs)-1 {
				return nil, "", fmt.Errorf("xpath %q: an attribute selector (@%s) must be the last segment", xpathArg, s[1:])
			}
			attr = s[1:]
			continue
		}
		if !xmlValidElemName(s) {
			return nil, "", fmt.Errorf("xpath segment %q is not a simple element name; this port does not support XPath predicates ([...]), wildcards (*), or functions", s)
		}
		elems = append(elems, s)
	}
	return elems, attr, nil
}

func xmlValidElemName(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '_' || r == '-' || r == '.' || r == ':':
		default:
			return false
		}
	}
	return true
}

// xmlNode is this port's mutable DOM node — deliberately simple (a
// single Text string per element, not full interleaved mixed content).
type xmlNode struct {
	Name     string
	Attrs    []xml.Attr
	Children []*xmlNode
	Text     string
}

// xmlParseDocument parses data into a tree, also capturing a leading
// "<?xml ...?>" declaration (if any) so it can be re-emitted verbatim.
func xmlParseDocument(data []byte) (decl string, root *xmlNode, err error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	var stack []*xmlNode
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", nil, err
		}
		switch t := tok.(type) {
		case xml.ProcInst:
			if t.Target == "xml" && root == nil {
				decl = "<?" + t.Target + " " + string(t.Inst) + "?>"
			}
		case xml.StartElement:
			n := &xmlNode{Name: t.Name.Local, Attrs: append([]xml.Attr{}, t.Attr...)}
			if len(stack) > 0 {
				parent := stack[len(stack)-1]
				parent.Children = append(parent.Children, n)
			} else if root == nil {
				root = n
			}
			stack = append(stack, n)
		case xml.EndElement:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		case xml.CharData:
			if len(stack) > 0 && strings.TrimSpace(string(t)) != "" {
				stack[len(stack)-1].Text += string(t)
			}
		}
	}
	if root == nil {
		return "", nil, fmt.Errorf("no root element found")
	}
	return decl, root, nil
}

// xmlSerializeDocument writes the tree back out, preceded by decl (or a
// generic "<?xml version=\"1.0\"?>" if none was captured).
func xmlSerializeDocument(decl string, root *xmlNode) ([]byte, error) {
	var buf bytes.Buffer
	if decl != "" {
		buf.WriteString(decl)
	} else {
		buf.WriteString(`<?xml version="1.0"?>`)
	}
	buf.WriteString("\n")
	enc := xml.NewEncoder(&buf)
	if err := xmlEncodeNode(enc, root); err != nil {
		return nil, err
	}
	if err := enc.Flush(); err != nil {
		return nil, err
	}
	buf.WriteString("\n")
	return buf.Bytes(), nil
}

func xmlEncodeNode(enc *xml.Encoder, n *xmlNode) error {
	start := xml.StartElement{Name: xml.Name{Local: n.Name}, Attr: n.Attrs}
	if err := enc.EncodeToken(start); err != nil {
		return err
	}
	if n.Text != "" {
		if err := enc.EncodeToken(xml.CharData([]byte(n.Text))); err != nil {
			return err
		}
	}
	for _, c := range n.Children {
		if err := xmlEncodeNode(enc, c); err != nil {
			return err
		}
	}
	return enc.EncodeToken(xml.EndElement{Name: start.Name})
}

// xmlFind resolves elems against root, requiring elems[0] (if any) to
// match the root element's own name, then walking each subsequent
// segment against the current node's direct children only (first match
// wins — no wildcards, no multiple matches). Returns the found node,
// its parent (nil for the root itself), and whether it was found.
func xmlFind(root *xmlNode, elems []string) (node, parent *xmlNode, ok bool) {
	if len(elems) == 0 {
		return root, nil, true
	}
	if elems[0] != root.Name {
		return nil, nil, false
	}
	cur := root
	var curParent *xmlNode
	for _, seg := range elems[1:] {
		next := xmlChildByName(cur, seg)
		if next == nil {
			return nil, nil, false
		}
		curParent = cur
		cur = next
	}
	return cur, curParent, true
}

// xmlEnsure is xmlFind's create-if-missing counterpart: it creates any
// missing intermediate (and final) elements as empty children, matching
// real xml's own documented "parent XML nodes are created
// automatically" behavior.
func xmlEnsure(root *xmlNode, elems []string) (node, parent *xmlNode, created bool, err error) {
	if len(elems) == 0 {
		return root, nil, false, nil
	}
	if elems[0] != root.Name {
		return nil, nil, false, fmt.Errorf("xpath's root element %q does not match the document's actual root element %q; this port cannot create or retarget the root", elems[0], root.Name)
	}
	cur := root
	var curParent *xmlNode
	for _, seg := range elems[1:] {
		next := xmlChildByName(cur, seg)
		if next == nil {
			next = &xmlNode{Name: seg}
			cur.Children = append(cur.Children, next)
			created = true
		}
		curParent = cur
		cur = next
	}
	return cur, curParent, created, nil
}

func xmlChildByName(n *xmlNode, name string) *xmlNode {
	for _, c := range n.Children {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func xmlGetAttr(n *xmlNode, name string) (string, bool) {
	for _, a := range n.Attrs {
		if a.Name.Local == name {
			return a.Value, true
		}
	}
	return "", false
}

func xmlSetAttr(n *xmlNode, name, value string) {
	for i, a := range n.Attrs {
		if a.Name.Local == name {
			n.Attrs[i].Value = value
			return
		}
	}
	n.Attrs = append(n.Attrs, xml.Attr{Name: xml.Name{Local: name}, Value: value})
}

func xmlRemoveAttr(n *xmlNode, name string) {
	out := n.Attrs[:0]
	for _, a := range n.Attrs {
		if a.Name.Local != name {
			out = append(out, a)
		}
	}
	n.Attrs = out
}

func xmlRemoveChild(parent, child *xmlNode) {
	var kept []*xmlNode
	for _, c := range parent.Children {
		if c != child {
			kept = append(kept, c)
		}
	}
	parent.Children = kept
}

// xmlMatchValue builds one "matches"/content entry for node — a
// simplified stand-in for real xml's own richer per-match dict, since
// this port only ever resolves a single node.
func xmlMatchValue(node *xmlNode, attribute string) any {
	if attribute != "" {
		v, _ := xmlGetAttr(node, attribute)
		return map[string]any{attribute: v}
	}
	return node.Text
}
