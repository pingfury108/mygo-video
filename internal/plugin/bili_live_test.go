package plugin

import (
	"os"
	"testing"
)

// TestBilibiliPlugin runs the example plugin against the real site when
// NETWORK is set and BILI_COOKIE holds a signed-in cookie.
func TestBilibiliPlugin(t *testing.T) {
	if os.Getenv("NETWORK") == "" {
		t.Skip("NETWORK is not set")
	}
	p, err := Load("../../examples/bilibili-plugin", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if c := os.Getenv("BILI_COOKIE"); c != "" {
		if err := p.SetSetting("cookie", c); err != nil {
			t.Fatal(err)
		}
	}
	home, err := p.Home()
	if err != nil {
		t.Fatal(err)
	}
	if len(home.Sections) == 0 || len(home.Sections[0].Videos) == 0 {
		t.Fatal("no videos on home")
	}
	first := home.Sections[0].Videos[0]
	t.Logf("home: %s (%s)", first.Name, first.ID)
	sr, err := p.Search("电影", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(sr.Videos) == 0 {
		t.Fatal("no search results")
	}
	t.Logf("search: %s (%s)", sr.Videos[0].Name, sr.Videos[0].ID)
	title, err := p.Title(sr.Videos[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("title: %s, %d episodes", title.Video.Name, len(title.Episodes))
	pb, err := p.Play(title.Video.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if pb.URL == "" {
		t.Fatal("no play url")
	}
	t.Logf("play: %s %s", pb.Type, pb.URL[:min(80, len(pb.URL))])
	if acc, err := p.Account(); err == nil && acc != nil {
		t.Logf("account: %s", acc.Name)
	}
}
