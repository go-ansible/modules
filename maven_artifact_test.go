package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestMavenArtifactURL(t *testing.T) {
	got := mavenArtifactURL("https://repo1.maven.org/maven2", "junit", "junit", "4.11", "", "jar")
	want := "https://repo1.maven.org/maven2/junit/junit/4.11/junit-4.11.jar"
	if got != want {
		t.Fatalf("url = %q, want %q", got, want)
	}
}

func TestMavenArtifactURLGroupIDDots(t *testing.T) {
	got := mavenArtifactURL("https://repo1.maven.org/maven2", "org.springframework", "spring-core", "5.3.0", "", "jar")
	want := "https://repo1.maven.org/maven2/org/springframework/spring-core/5.3.0/spring-core-5.3.0.jar"
	if got != want {
		t.Fatalf("url = %q, want %q", got, want)
	}
}

func TestMavenArtifactURLClassifierAndExtension(t *testing.T) {
	got := mavenArtifactURL("https://repo.company.com/maven/", "com.company", "web-app", "1.0", "sources", "war")
	want := "https://repo.company.com/maven/com/company/web-app/1.0/web-app-1.0-sources.war"
	if got != want {
		t.Fatalf("url = %q, want %q", got, want)
	}
}

func TestModuleMavenArtifactDownloadsWhenMissing(t *testing.T) {
	dest := "/opt/junit.jar"
	url := mavenArtifactURL("https://repo1.maven.org/maven2", "junit", "junit", "4.11", "", "jar")
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e " + shellQuote(dest): {RC: 1},
		getURLCmd(dest, url):          {RC: 0},
	})
	res, err := moduleMavenArtifact(context.Background(), conn, map[string]any{
		"group_id": "junit", "artifact_id": "junit", "version": "4.11", "dest": dest,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["url"] != url {
		t.Fatalf("url extra = %v, want %v", res.Extra["url"], url)
	}
}

func TestModuleMavenArtifactSkipsWhenPresent(t *testing.T) {
	dest := "/opt/junit.jar"
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e " + shellQuote(dest): {RC: 0},
	})
	res, err := moduleMavenArtifact(context.Background(), conn, map[string]any{
		"group_id": "junit", "artifact_id": "junit", "version": "4.11", "dest": dest,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged when dest already exists")
	}
	if len(conn.Commands) != 1 {
		t.Fatalf("want no download attempted, commands = %v", conn.Commands)
	}
}

func TestModuleMavenArtifactDownloadFails(t *testing.T) {
	dest := "/opt/junit.jar"
	url := mavenArtifactURL("https://repo1.maven.org/maven2", "junit", "junit", "4.11", "", "jar")
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e " + shellQuote(dest): {RC: 1},
		getURLCmd(dest, url):          {RC: 22, Stderr: "curl: (22) HTTP 404"},
	})
	res, err := moduleMavenArtifact(context.Background(), conn, map[string]any{
		"group_id": "junit", "artifact_id": "junit", "version": "4.11", "dest": dest,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed when download fails")
	}
}

func TestModuleMavenArtifactCustomRepositoryURL(t *testing.T) {
	dest := "/opt/lib.jar"
	url := "https://repo.company.com/maven/com/company/library-name/1.0/library-name-1.0.jar"
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e " + shellQuote(dest): {RC: 1},
		getURLCmd(dest, url):          {RC: 0},
	})
	res, err := moduleMavenArtifact(context.Background(), conn, map[string]any{
		"group_id": "com.company", "artifact_id": "library-name", "version": "1.0",
		"repository_url": "https://repo.company.com/maven", "dest": dest,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleMavenArtifactAbsentRemoves(t *testing.T) {
	dest := "/opt/junit.jar"
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e " + shellQuote(dest): {RC: 0},
		"rm -f " + shellQuote(dest):   {RC: 0},
	})
	res, err := moduleMavenArtifact(context.Background(), conn, map[string]any{
		"group_id": "junit", "artifact_id": "junit", "version": "4.11", "dest": dest, "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed when removing an existing artifact")
	}
}

func TestModuleMavenArtifactAbsentAlreadyGone(t *testing.T) {
	dest := "/opt/junit.jar"
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e " + shellQuote(dest): {RC: 1},
	})
	res, err := moduleMavenArtifact(context.Background(), conn, map[string]any{
		"group_id": "junit", "artifact_id": "junit", "version": "4.11", "dest": dest, "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged when dest is already absent")
	}
}

func TestModuleMavenArtifactMode(t *testing.T) {
	dest := "/opt/junit.jar"
	url := mavenArtifactURL("https://repo1.maven.org/maven2", "junit", "junit", "4.11", "", "jar")
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e " + shellQuote(dest): {RC: 1},
		getURLCmd(dest, url):          {RC: 0},
		"stat -c '%s|%a|%F' " + dest + " 2>/dev/null || stat -f '%z|%Lp|%HT' " + dest + " 2>/dev/null": {
			RC: 0, Stdout: "10|644|regular file\n",
		},
		"chmod 0755 " + dest: {RC: 0},
	})
	res, err := moduleMavenArtifact(context.Background(), conn, map[string]any{
		"group_id": "junit", "artifact_id": "junit", "version": "4.11", "dest": dest, "mode": "0755",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed for a mode change")
	}
}

func TestModuleMavenArtifactMissingArgs(t *testing.T) {
	conn := newFakeConn(nil)
	base := map[string]any{"group_id": "junit", "artifact_id": "junit", "version": "4.11", "dest": "/x"}
	for _, missing := range []string{"group_id", "artifact_id", "version", "dest"} {
		args := map[string]any{}
		for k, v := range base {
			if k != missing {
				args[k] = v
			}
		}
		if _, err := moduleMavenArtifact(context.Background(), conn, args); err == nil {
			t.Fatalf("want error for missing %s", missing)
		}
	}
}

func TestModuleMavenArtifactBadState(t *testing.T) {
	conn := newFakeConn(nil)
	_, err := moduleMavenArtifact(context.Background(), conn, map[string]any{
		"group_id": "junit", "artifact_id": "junit", "version": "4.11", "dest": "/x", "state": "latest",
	})
	if err == nil {
		t.Fatal("want error for invalid state")
	}
}
