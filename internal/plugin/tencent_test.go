package plugin

import (
	"os"
	"strings"
	"testing"
)

// TestTencentPlugin reads the real channel pages when NETWORK is set.
func TestTencentPlugin(t *testing.T) {
	if os.Getenv("NETWORK") == "" {
		t.Skip("NETWORK is not set")
	}
	p, err := Load("../../examples/tencent-plugin", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	home, err := p.Home()
	if err != nil {
		t.Fatal(err)
	}
	if len(home.Sections) == 0 {
		t.Fatal("no sections on home")
	}
	for _, s := range home.Sections {
		t.Logf("home %q: %d videos", s.Title, len(s.Videos))
	}
	first := home.Sections[0].Videos[0]
	if first.Name == "" {
		t.Fatal("first video has no name")
	}
	if first.Poster == "" {
		t.Error("first video has no poster")
	}
	t.Logf("first: %s (%s) %s", first.Name, first.ID, first.Poster)

	detail, err := p.Title(first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Video.Name != first.Name {
		t.Errorf("title %q, want %q", detail.Video.Name, first.Name)
	}
	pb, err := p.Play(first.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(pb.Open, "v.qq.com/x/cover/") {
		t.Errorf("opens %q", pb.Open)
	}
	t.Logf("play opens %s", pb.Open)

	sr, err := p.Search("三体", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(sr.Videos) != 1 || !strings.Contains(sr.Videos[0].Name, "三体") {
		t.Errorf("search gave %+v", sr.Videos)
	}
	pb, err = p.Play(sr.Videos[0].ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(pb.Open, "x/search/") {
		t.Errorf("search opens %q", pb.Open)
	}
}
