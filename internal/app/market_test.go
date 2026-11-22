package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mygo-video/internal/store"
)

func TestParseMarket(t *testing.T) {
	plugins, err := parseMarket([]byte(`{
	  "schema": 1,
	  "plugins": [
	    {"id":"demo","name":"Demo","version":"0.1.0","asset":"plugin-demo.zip","sha256":"abc"},
	    {"name":"No id"},
	    {"id":"no-asset","name":"No asset"}
	  ]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(plugins) != 1 || plugins[0].ID != "demo" {
		t.Fatalf("plugins %+v", plugins)
	}
	if _, err := parseMarket([]byte(`{"schema":2,"plugins":[]}`)); err == nil {
		t.Fatal("a newer market schema was accepted")
	}
}

func writeTestPlugin(t *testing.T, dir, version string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"id":"demo","name":"Demo","version":"` + version + `","apiVersion":1,"main":"index.js"}`
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.js"), []byte("export function home() {}"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestInstallDirReplacesPlugin(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	a := &app{
		store:         st,
		pluginsDir:    filepath.Join(dir, "plugins"),
		pluginDataDir: filepath.Join(dir, "plugin-data"),
	}
	if err := os.MkdirAll(a.pluginsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	first := filepath.Join(dir, "first")
	writeTestPlugin(t, first, "0.1.0")
	if _, err := a.installDirAt(first, "test", true); err != nil {
		t.Fatal(err)
	}

	second := filepath.Join(dir, "second")
	writeTestPlugin(t, second, "0.2.0")
	if _, err := a.installDirAt(second, "test", true); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(a.pluginsDir, "demo", "plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"0.2.0"`) {
		t.Fatalf("installed manifest %s", body)
	}
	entries, err := os.ReadDir(a.pluginsDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "demo" {
		t.Fatalf("plugin folder %v", entries)
	}
}
