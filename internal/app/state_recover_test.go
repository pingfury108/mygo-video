package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"mygo-video/internal/media"
	"mygo-video/internal/store"
)

func TestRecoverNestedStateFillsMissingProgress(t *testing.T) {
	defaultDir := t.TempDir()
	nested := filepath.Join(defaultDir, baseName())
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	deepest := filepath.Join(nested, baseName())
	oldRoot := filepath.Join(deepest, "videos")
	if err := os.MkdirAll(oldRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	writeStateFile(t, filepath.Join(defaultDir, "state.json"), store.Data{
		Progress: map[string]media.Progress{
			"webdav:one": {Position: 1000, Duration: 2000, Updated: 10},
			"webdav:two": {Position: 1500, Duration: 3000, Updated: 20},
		},
		Volume:     0.8,
		WebDAV:     store.WebDAVConfig{URL: "http://example.test/"},
		AVOffsetMs: -40,
		Plugins: map[string]store.PluginState{
			"demo": {Enabled: true, Origin: "market"},
		},
		Roots: []string{oldRoot, filepath.Join(defaultDir, "Desktop")},
	})
	writeStateFile(t, filepath.Join(target, "state.json"), store.Data{
		Volume: 0.6,
		Roots:  []string{oldRoot},
	})

	recoverNestedState(target, defaultDir)

	got := readStateFile(t, filepath.Join(target, "state.json"))
	if len(got.Progress) != 2 {
		t.Fatalf("progress entries = %d, want 2", len(got.Progress))
	}
	if got.Progress["webdav:one"].Position != 1000 {
		t.Fatalf("webdav:one = %+v", got.Progress["webdav:one"])
	}
	if got.WebDAV.URL != "http://example.test/" {
		t.Fatalf("webdav URL = %q", got.WebDAV.URL)
	}
	if got.AVOffsetMs != -40 {
		t.Fatalf("avOffsetMs = %d, want -40", got.AVOffsetMs)
	}
	if !got.Plugins["demo"].Enabled {
		t.Fatalf("plugin state = %+v, want enabled", got.Plugins["demo"])
	}
	if got.Volume != 0.6 {
		t.Fatalf("volume = %v, want the chosen directory's 0.6", got.Volume)
	}
	if len(got.Roots) != 1 || !sameDir(got.Roots[0], filepath.Join(target, "videos")) {
		t.Fatalf("roots = %v, want the chosen directory's videos", got.Roots)
	}
	if _, err := os.Stat(filepath.Join(target, "state.json.before-recover.bak")); err != nil {
		t.Fatalf("missing backup: %v", err)
	}
}

func TestRecoverNestedStateLeavesUsedDirectoryAlone(t *testing.T) {
	defaultDir := t.TempDir()
	target := t.TempDir()
	writeStateFile(t, filepath.Join(defaultDir, "state.json"), store.Data{
		Progress: map[string]media.Progress{
			"webdav:old": {Position: 10, Updated: 1},
		},
	})
	path := filepath.Join(target, "state.json")
	writeStateFile(t, path, store.Data{
		Progress: map[string]media.Progress{
			"webdav:new": {Position: 20, Updated: 2},
		},
	})
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	recoverNestedState(target, defaultDir)

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("state changed:\n%s\nwant:\n%s", after, before)
	}
}

func TestRecoveredRootsCollapsesRepeatedDataDir(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, baseName(), baseName(), "videos")
	if err := os.MkdirAll(bad, 0o700); err != nil {
		t.Fatal(err)
	}

	got := recoveredRoots([]string{bad}, nil, dir)

	if len(got) != 1 || !sameDir(got[0], filepath.Join(dir, "videos")) {
		t.Fatalf("roots = %v, want the data directory's videos", got)
	}
}

func writeStateFile(t *testing.T, path string, data store.Data) {
	t.Helper()
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, path, string(b))
}

func readStateFile(t *testing.T, path string) store.Data {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var data store.Data
	if err := json.Unmarshal(b, &data); err != nil {
		t.Fatal(err)
	}
	return data
}
