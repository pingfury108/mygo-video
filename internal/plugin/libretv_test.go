package plugin

import (
	"os"
	"strings"
	"testing"
)

// TestLibreTVPlugin runs the example plugin against the real sites when
// NETWORK is set.
func TestLibreTVPlugin(t *testing.T) {
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
	if len(sr.Videos) == 0 {
		t.Fatal("no search results")
	}
	t.Logf("search: %d videos, first %q (%s), more=%v",
		len(sr.Videos), sr.Videos[0].Name, sr.Videos[0].ID, sr.More)

	detail, err := p.Title(sr.Videos[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Episodes) == 0 {
		t.Fatal("no episodes")
	}
	t.Logf("title %q: %d episodes, first %q",
		detail.Video.Name, len(detail.Episodes), detail.Episodes[0].Name)

	pb, err := p.Play(detail.Episodes[0].ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if pb.URL == "" {
		t.Fatalf("no stream: %+v", pb)
	}
	if pb.Type != "hls" || !strings.Contains(pb.URL, ".m3u8") {
		t.Errorf("play %q as %q", pb.URL, pb.Type)
	}
	t.Logf("play %s", pb.URL)
}
