package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSourceKept(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.SetSource("plugin:demo")
	again, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := again.Source(); got != "plugin:demo" {
		t.Fatalf("source = %q, want plugin:demo", got)
	}
}

// A file from before sources had names kept the index of the source.
func TestSourceFromIndex(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	if err := os.WriteFile(path, []byte(`{"source":2}`), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Source(); got != "iptv" {
		t.Fatalf("source = %q, want iptv", got)
	}
}

func TestPluginEnabledKept(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.SetPluginInstalled("demo", "folder")
	s.SetPluginEnabled("demo", true)
	again, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	st := again.Plugins()["demo"]
	if !st.Enabled || st.Origin != "folder" || st.InstalledAt == 0 || st.EnabledAt == 0 {
		t.Fatalf("state = %+v", st)
	}
	again.SetPluginEnabled("demo", false)
	if again.Plugins()["demo"].Enabled {
		t.Fatal("plugin still enabled")
	}
	again.ForgetPlugin("demo")
	if _, ok := again.Plugins()["demo"]; ok {
		t.Fatal("plugin not forgotten")
	}
}
