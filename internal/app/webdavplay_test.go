package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// With WEBDAV_VIDEO set to a file on a server, downloads it into the cache.
func TestWebDAVDownload(t *testing.T) {
	raw := os.Getenv("WEBDAV_VIDEO")
	if raw == "" {
		t.Skip("WEBDAV_VIDEO is not set")
	}
	a := &app{dataDir: t.TempDir(), store: freshStore(t)}
	path := filepath.Join(a.dataDir, "cache", "probe.mp4")
	if err := a.fetchTo(context.Background(), raw, path, nil); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Size() == 0 {
		t.Fatal("empty download")
	}
	t.Logf("downloaded %d bytes", st.Size())
}
