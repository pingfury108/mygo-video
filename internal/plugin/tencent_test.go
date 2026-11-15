package plugin

import (
	"strings"
	"testing"
)

// TestTencentPlugin checks the catalog plugin's own shape; it makes no
// requests, because every card is a page to open.
func TestTencentPlugin(t *testing.T) {
	p, err := Load("../../examples/tencent-plugin", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	home, err := p.Home()
	if err != nil {
		t.Fatal(err)
	}
	if len(home.Sections) == 0 || len(home.Sections[0].Videos) < 5 {
		t.Fatalf("home has %d sections", len(home.Sections))
	}
	t.Logf("home: %d channels, first %q", len(home.Sections[0].Videos), home.Sections[0].Videos[0].Name)

	sr, err := p.Search("三体", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(sr.Videos) != 1 {
		t.Fatalf("search returned %d", len(sr.Videos))
	}
	t.Logf("search: %s", sr.Videos[0].Name)

	detail, err := p.Title(sr.Videos[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(detail.Video.Name, "三体") {
		t.Errorf("title %q", detail.Video.Name)
	}

	pb, err := p.Play(detail.Video.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(pb.Open, "v.qq.com") || !strings.Contains(pb.Open, "%E4%B8%89%E4%BD%93") {
		t.Errorf("opens %q", pb.Open)
	}
	t.Logf("play opens %s", pb.Open)
}
