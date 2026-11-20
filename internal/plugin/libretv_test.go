package plugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// copyPlugin copies an example plugin into dir, with the given replacements
// applied to its index.js.
func copyPlugin(t *testing.T, from, dir string, repl ...string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	js, err := os.ReadFile(filepath.Join(from, "index.js"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(js)
	for i := 0; i+1 < len(repl); i += 2 {
		body = strings.Replace(body, repl[i], repl[i+1], 1)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.js"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	man, err := os.ReadFile(filepath.Join(from, "plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), man, 0o644); err != nil {
		t.Fatal(err)
	}
}

// The sources the plugin carries inside, for the case the deployed site's
// list cannot be reached.
var builtIn = map[string]bool{
	"ruyi": true, "bfzy": true, "zy360": true, "jisu": true, "lzi": true,
}

// TestLibreTVListDown plays with the list address pointing at a dead port:
// the plugin has to fall back to its own list and still search, resolve a
// title and hand a stream over.
func TestLibreTVListDown(t *testing.T) {
	if os.Getenv("NETWORK") == "" {
		t.Skip("NETWORK is not set")
	}
	dir := filepath.Join(t.TempDir(), "libretv")
	copyPlugin(t, "../../examples/libretv-plugin", dir,
		"https://tv.pingfury.top/js/customer_site.js", "http://127.0.0.1:9/customer_site.js")
	p, err := Load(dir, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	sr, err := p.Search("庆余年", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(sr.Videos) == 0 {
		t.Fatal("no videos with the list down")
	}
	for _, v := range sr.Videos {
		key := strings.SplitN(v.ID, "|", 2)[0]
		if !builtIn[key] {
			t.Fatalf("used a source outside the built-in list: %s", key)
		}
	}
	t.Logf("fallback: %d videos from the built-in list", len(sr.Videos))

	detail, err := p.Title(sr.Videos[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Episodes) == 0 {
		t.Fatal("no episodes")
	}
	pb, err := p.Play(detail.Episodes[0].ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if pb.URL == "" || pb.Type != "hls" {
		t.Fatalf("play %+v", pb)
	}
	t.Logf("fallback play %s", pb.URL)
}

// TestLibreTVListUp runs against the real deployment: the fetched list has
// to bring in sources beyond the ones the plugin carries inside.
func TestLibreTVListUp(t *testing.T) {
	if os.Getenv("NETWORK") == "" {
		t.Skip("NETWORK is not set")
	}
	p, err := Load("../../examples/libretv-plugin", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	sr, err := p.Search("庆余年", 0)
	if err != nil {
		t.Fatal(err)
	}
	extra := map[string]bool{}
	for _, v := range sr.Videos {
		key := strings.SplitN(v.ID, "|", 2)[0]
		if !builtIn[key] {
			extra[key] = true
		}
	}
	if len(extra) == 0 {
		t.Fatal("the fetched list added no source beyond the built-in ones")
	}
	names := ""
	for k := range extra {
		names += k + " "
	}
	t.Logf("fetched list brought: %s(%d videos total)", strings.TrimSpace(names), len(sr.Videos))
}
