package plugin

import (
	"os"
	"strings"
	"testing"

	"mygo-video/internal/media"
)

// TestYoukuPlugin runs the example plugin against the real site when NETWORK
// is set.
func TestYoukuPlugin(t *testing.T) {
	if os.Getenv("NETWORK") == "" {
		t.Skip("NETWORK is not set")
	}
	p, err := Load("../../examples/youku-plugin", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	home, err := p.Home()
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, s := range home.Sections {
		t.Logf("home %q: %d videos, first %q", s.Title, len(s.Videos), first(s))
		total += len(s.Videos)
	}
	if total == 0 {
		t.Fatal("no videos on home")
	}

	sr, err := p.Search("三体", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(sr.Videos) == 0 {
		t.Fatal("no search results")
	}
	t.Logf("search: %s (%s), more=%v", sr.Videos[0].Name, sr.Videos[0].ID, sr.More)

	detail, err := p.Title(sr.Videos[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Video.Name != sr.Videos[0].Name {
		t.Errorf("title %q, want %q", detail.Video.Name, sr.Videos[0].Name)
	}

	pb, err := p.Play(detail.Video.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if pb.Open == "" {
		t.Fatalf("no page to open: %+v", pb)
	}
	if !strings.Contains(pb.Open, "v.youku.com/v_show/id_") {
		t.Errorf("opened %q", pb.Open)
	}
	t.Logf("play opens %s", pb.Open)
}

func first(s media.Section) string {
	if len(s.Videos) == 0 {
		return ""
	}
	return s.Videos[0].Name
}
