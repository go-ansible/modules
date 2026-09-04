package modules

import "testing"

func TestGlabEncodeID(t *testing.T) {
	if got := glabEncodeID("group/subgroup/project"); got != "group%2Fsubgroup%2Fproject" {
		t.Fatalf("glabEncodeID = %q", got)
	}
	if got := glabEncodeID("42"); got != "42" {
		t.Fatalf("glabEncodeID(numeric) = %q", got)
	}
}

func TestGlabAccessLevel(t *testing.T) {
	cases := map[string]int{
		"nobody": 0, "no one": 0, "guest": 10, "planner": 15, "reporter": 20,
		"developer": 30, "maintainer": 40, "master": 40, "owner": 50,
	}
	for name, want := range cases {
		got, err := glabAccessLevel(name)
		if err != nil {
			t.Fatalf("glabAccessLevel(%q): %v", name, err)
		}
		if got != want {
			t.Fatalf("glabAccessLevel(%q) = %d, want %d", name, got, want)
		}
	}
	if _, err := glabAccessLevel("bogus"); err == nil {
		t.Fatal("want error for unknown access level")
	}
}

func TestGlabIsNotFound(t *testing.T) {
	if !glabIsNotFound(glabResult{RC: 1, Stderr: "404 Not Found"}) {
		t.Fatal("want true for a 404 stderr")
	}
	if glabIsNotFound(glabResult{RC: 0, Stdout: "ok"}) {
		t.Fatal("want false for a successful result")
	}
	if glabIsNotFound(glabResult{RC: 1, Stderr: "500 Internal Server Error"}) {
		t.Fatal("want false for a non-404 failure")
	}
}
