package app

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestParseRepo(t *testing.T) {
	cases := []struct {
		in, owner, repo string
		ok              bool
	}{
		{"egoist/mygo-music", "egoist", "mygo-music", true},
		{"https://github.com/egoist/mygo-music", "egoist", "mygo-music", true},
		{"https://github.com/egoist/mygo-music.git", "egoist", "mygo-music", true},
		{"github.com/egoist/mygo-music/", "egoist", "mygo-music", true},
		{"https://github.com/egoist/mygo-music/tree/main", "egoist", "mygo-music", true},
		{"nope", "", "", false},
		{"", "", "", false},
	}
	for _, c := range cases {
		owner, repo, err := parseRepo(c.in)
		if c.ok && (err != nil || owner != c.owner || repo != c.repo) {
			t.Errorf("parseRepo(%q) = %q %q %v", c.in, owner, repo, err)
		}
		if !c.ok && err == nil {
			t.Errorf("parseRepo(%q) should fail", c.in)
		}
	}
}

func makeZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// A zip with a folder in it installs from the folder that holds plugin.json.
func TestUnzipAndFindRoot(t *testing.T) {
	data := makeZip(t, map[string]string{
		"repo-main/README.md":      "hi",
		"repo-main/plugin.json":    `{"id":"demo"}`,
		"repo-main/index.js":       "export function home(){}",
		"repo-main/locales/x.json": "{}",
	})
	dir := t.TempDir()
	if err := unzip(data, dir); err != nil {
		t.Fatal(err)
	}
	root, err := findPluginRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(root) != "repo-main" {
		t.Fatalf("root %q", root)
	}
	if _, err := os.Stat(filepath.Join(root, "index.js")); err != nil {
		t.Fatal(err)
	}
}

func TestUnzipRejectsEscape(t *testing.T) {
	data := makeZip(t, map[string]string{"../evil.js": "x"})
	if err := unzip(data, t.TempDir()); err == nil {
		t.Fatal("zip slip was accepted")
	}
}

func TestFindPluginRootMissing(t *testing.T) {
	if _, err := findPluginRoot(t.TempDir()); err == nil {
		t.Fatal("a folder without plugin.json was accepted")
	}
}

// The release asset named for this platform wins, then one named for the
// system, then any zip.
func TestPickAsset(t *testing.T) {
	assets := []releaseAsset{
		{Name: "plugin-linux-amd64.zip", URL: "linux"},
		{Name: "plugin-" + runtime.GOOS + "-" + runtime.GOARCH + ".zip", URL: "here"},
		{Name: "plugin.zip", URL: "any"},
	}
	if got := pickAsset(assets); got != "here" {
		t.Fatalf("picked %q", got)
	}
	if got := pickAsset([]releaseAsset{{Name: "plugin.zip", URL: "any"}}); got != "any" {
		t.Fatalf("picked %q", got)
	}
	if got := pickAsset([]releaseAsset{{Name: "plugin.tar.gz", URL: "tgz"}}); got != "" {
		t.Fatalf("picked %q", got)
	}
}

// A plugin that doesn't load is turned away before it is copied.
func TestInstallDirRejects(t *testing.T) {
	a := &app{
		pluginsDir:    t.TempDir(),
		pluginDataDir: t.TempDir(),
	}
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "plugin.json"), []byte(`{"id":"x","main":"index.js"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := a.installDir(src); err == nil || !strings.Contains(err.Error(), "插件") {
		t.Fatalf("err %v", err)
	}
}

// With NETWORK set, downloads a real repository from GitHub and finds the
// plugin in it.
func TestDownloadFromGitHub(t *testing.T) {
	if os.Getenv("NETWORK") == "" {
		t.Skip("NETWORK is not set")
	}
	data, err := downloadPluginZip("egoist", "mygo-music")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := unzip(data, dir); err != nil {
		t.Fatal(err)
	}
	root, err := findPluginRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "plugin.json")); err != nil {
		t.Fatal(err)
	}
}
