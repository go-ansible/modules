package modules

import (
	"context"
	"testing"
)

func TestModuleXmlInfoCount(t *testing.T) {
	conn := local()
	f := writeXMLFixture(t)
	res, err := moduleXmlInfo(context.Background(), conn, map[string]any{
		"path": f, "xpath": "/business/rating", "what": "count",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("xml_info must never report Changed")
	}
	if res.Extra["count"] != 1 {
		t.Fatalf("count = %v", res.Extra["count"])
	}
}

func TestModuleXmlInfoCountNoMatch(t *testing.T) {
	conn := local()
	f := writeXMLFixture(t)
	res, err := moduleXmlInfo(context.Background(), conn, map[string]any{
		"path": f, "xpath": "/business/nonexistent", "what": "count",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Extra["count"] != 0 {
		t.Fatalf("count = %v", res.Extra["count"])
	}
}

func TestModuleXmlInfoPaths(t *testing.T) {
	conn := local()
	f := writeXMLFixture(t)
	res, err := moduleXmlInfo(context.Background(), conn, map[string]any{
		"path": f, "xpath": "/business/rating", "what": "paths",
	})
	if err != nil {
		t.Fatal(err)
	}
	matches, ok := res.Extra["matches"].([]any)
	if !ok || len(matches) != 1 || matches[0] != "/business/rating[1]" {
		t.Fatalf("matches = %v", res.Extra["matches"])
	}
}

func TestModuleXmlInfoContentText(t *testing.T) {
	conn := local()
	f := writeXMLFixture(t)
	res, err := moduleXmlInfo(context.Background(), conn, map[string]any{
		"path": f, "xpath": "/business/name", "what": "content_text",
	})
	if err != nil {
		t.Fatal(err)
	}
	entries, ok := res.Extra["content_text"].([]any)
	if !ok || len(entries) != 1 {
		t.Fatalf("content_text = %v", res.Extra["content_text"])
	}
	entry := entries[0].(map[string]any)
	if entry["tag"] != "name" || entry["text"] != "Tasty Beverage Co." {
		t.Fatalf("entry = %v", entry)
	}
}

func TestModuleXmlInfoContentAttributes(t *testing.T) {
	conn := local()
	f := writeXMLFixture(t)
	res, err := moduleXmlInfo(context.Background(), conn, map[string]any{
		"path": f, "xpath": "/business/rating", "what": "content_attributes",
	})
	if err != nil {
		t.Fatal(err)
	}
	entries, ok := res.Extra["content_attributes"].([]any)
	if !ok || len(entries) != 1 {
		t.Fatalf("content_attributes = %v", res.Extra["content_attributes"])
	}
	entry := entries[0].(map[string]any)
	attrs := entry["attributes"].(map[string]any)
	if attrs["subjective"] != "true" {
		t.Fatalf("attrs = %v", attrs)
	}
}

func TestModuleXmlInfoContentTextNoMatchFails(t *testing.T) {
	conn := local()
	f := writeXMLFixture(t)
	res, err := moduleXmlInfo(context.Background(), conn, map[string]any{
		"path": f, "xpath": "/business/nonexistent", "what": "content_text",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed when xpath does not reference a node")
	}
}

func TestModuleXmlInfoXmlstring(t *testing.T) {
	conn := local()
	res, err := moduleXmlInfo(context.Background(), conn, map[string]any{
		"xmlstring": `<config><item>1</item></config>`, "xpath": "/config/item", "what": "count",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Extra["count"] != 1 {
		t.Fatalf("count = %v", res.Extra["count"])
	}
}

func TestModuleXmlInfoValidation(t *testing.T) {
	conn := local()
	if _, err := moduleXmlInfo(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing what")
	}
	if _, err := moduleXmlInfo(context.Background(), conn, map[string]any{"what": "count"}); err == nil {
		t.Fatal("want error for missing xpath")
	}
	if _, err := moduleXmlInfo(context.Background(), conn, map[string]any{"what": "count", "xpath": "/"}); err == nil {
		t.Fatal("want error: neither path nor xmlstring given")
	}
	if _, err := moduleXmlInfo(context.Background(), conn, map[string]any{
		"what": "count", "xpath": "/", "path": "/a", "xmlstring": "<a/>",
	}); err == nil {
		t.Fatal("want error: path and xmlstring mutually exclusive")
	}
	if _, err := moduleXmlInfo(context.Background(), conn, map[string]any{
		"what": "count", "xpath": "/a/@attr", "xmlstring": "<a/>",
	}); err == nil {
		t.Fatal("want error: attribute-selecting xpath rejected")
	}
	if _, err := moduleXmlInfo(context.Background(), conn, map[string]any{
		"what": "count", "xpath": "/a", "xmlstring": "<a/>", "namespaces": map[string]any{"x": "http://x.test"},
	}); err == nil {
		t.Fatal("want error: non-empty namespaces rejected")
	}
}
