package modules

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const xmlFixture = `<?xml version="1.0"?>
<business type="bar">
  <name>Tasty Beverage Co.</name>
  <rating subjective="true">10</rating>
</business>
`

func writeXMLFixture(t *testing.T) string {
	t.Helper()
	f := filepath.Join(t.TempDir(), "bar.xml")
	if err := os.WriteFile(f, []byte(xmlFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestXmlSplitPath(t *testing.T) {
	elems, attr, err := xmlSplitPath("/business/rating/@subjective")
	if err != nil {
		t.Fatal(err)
	}
	if attr != "subjective" {
		t.Errorf("attr = %q, want subjective", attr)
	}
	if len(elems) != 2 || elems[0] != "business" || elems[1] != "rating" {
		t.Errorf("elems = %v", elems)
	}
}

func TestXmlSplitPathRejectsPredicates(t *testing.T) {
	if _, _, err := xmlSplitPath("/config/element[@name='test1']"); err == nil {
		t.Fatal("want error: predicates not supported")
	}
}

func TestXmlSplitPathRejectsRelative(t *testing.T) {
	if _, _, err := xmlSplitPath("business/rating"); err == nil {
		t.Fatal("want error: relative xpath not supported")
	}
}

func TestModuleXmlGetText(t *testing.T) {
	f := writeXMLFixture(t)
	conn := local()
	res, err := moduleXml(context.Background(), conn, map[string]any{
		"path": f, "xpath": "/business/name", "content": "text",
	})
	if err != nil {
		t.Fatal(err)
	}
	matches, ok := res.Extra["matches"].([]any)
	if !ok || len(matches) != 1 || matches[0] != "Tasty Beverage Co." {
		t.Fatalf("matches = %v", res.Extra["matches"])
	}
	if res.Changed {
		t.Fatal("content read must not change anything")
	}
}

func TestModuleXmlSetText(t *testing.T) {
	f := writeXMLFixture(t)
	conn := local()
	res, err := moduleXml(context.Background(), conn, map[string]any{
		"path": f, "xpath": "/business/rating", "value": "11",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	data, _ := os.ReadFile(f)
	if !strings.Contains(string(data), "<rating subjective=\"true\">11</rating>") {
		t.Fatalf("content = %s", data)
	}

	// Re-running must be a no-op.
	res2, err := moduleXml(context.Background(), conn, map[string]any{
		"path": f, "xpath": "/business/rating", "value": "11",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res2.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleXmlRemoveAttribute(t *testing.T) {
	f := writeXMLFixture(t)
	conn := local()
	res, err := moduleXml(context.Background(), conn, map[string]any{
		"path": f, "xpath": "/business/rating/@subjective", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	data, _ := os.ReadFile(f)
	if strings.Contains(string(data), "subjective") {
		t.Fatalf("content = %s, want subjective attribute removed", data)
	}
}

func TestModuleXmlSetAttribute(t *testing.T) {
	f := writeXMLFixture(t)
	conn := local()
	res, err := moduleXml(context.Background(), conn, map[string]any{
		"path": f, "xpath": "/business", "attribute": "validatedon", "value": "1976-08-05",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	data, _ := os.ReadFile(f)
	if !strings.Contains(string(data), `validatedon="1976-08-05"`) {
		t.Fatalf("content = %s", data)
	}
}

func TestModuleXmlCreateMissingElement(t *testing.T) {
	f := writeXMLFixture(t)
	conn := local()
	res, err := moduleXml(context.Background(), conn, map[string]any{
		"path": f, "xpath": "/business/phonenumber", "value": "555-555-1234",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	data, _ := os.ReadFile(f)
	if !strings.Contains(string(data), "<phonenumber>555-555-1234</phonenumber>") {
		t.Fatalf("content = %s", data)
	}
}

func TestModuleXmlCreateIfMissingFalseNoOp(t *testing.T) {
	f := writeXMLFixture(t)
	conn := local()
	res, err := moduleXml(context.Background(), conn, map[string]any{
		"path": f, "xpath": "/business/phonenumber", "value": "555-555-1234", "create_if_missing": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged: create_if_missing is false")
	}
}

func TestModuleXmlCount(t *testing.T) {
	f := writeXMLFixture(t)
	conn := local()
	res, err := moduleXml(context.Background(), conn, map[string]any{
		"path": f, "xpath": "/business/rating", "count": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Extra["count"] != 1 {
		t.Fatalf("count = %v, want 1", res.Extra["count"])
	}

	res2, err := moduleXml(context.Background(), conn, map[string]any{
		"path": f, "xpath": "/business/nonexistent", "count": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res2.Extra["count"] != 0 {
		t.Fatalf("count = %v, want 0", res2.Extra["count"])
	}
}

func TestModuleXmlMissingFile(t *testing.T) {
	conn := local()
	res, err := moduleXml(context.Background(), conn, map[string]any{
		"path": filepath.Join(t.TempDir(), "absent.xml"), "xpath": "/a",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want failed: file must exist ahead of time")
	}
}

func TestModuleXmlMissingPath(t *testing.T) {
	conn := local()
	if _, err := moduleXml(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing path")
	}
}
