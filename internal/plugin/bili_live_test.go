package plugin

import (
	"os"
	"testing"

	"mygo-video/internal/media"
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
	if pb.URL == "" && (pb.Dash == nil || pb.Dash.Video == "") {
		t.Fatal("no play url")
	}
	if pb.Dash != nil {
		t.Logf("play: %s %s + %s", pb.Type, pb.Quality, pb.Dash.Video[:min(70, len(pb.Dash.Video))])
	} else {
		t.Logf("play: %s %s %s", pb.Type, pb.Quality, pb.URL[:min(70, len(pb.URL))])
	}
	if acc, err := p.Account(); err == nil && acc != nil {
		t.Logf("account: %s", acc.Name)
	}
}

// TestBilibiliLibrary reads the signed-in listener's pages.
func TestBilibiliLibrary(t *testing.T) {
	if os.Getenv("NETWORK") == "" || os.Getenv("BILI_COOKIE") == "" {
		t.Skip("NETWORK and BILI_COOKIE are not set")
	}
	p, err := Load("../../examples/bilibili-plugin", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if err := p.SetSetting("cookie", os.Getenv("BILI_COOKIE")); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name string
		load func() (*media.Browse, error)
	}{
		{"history", p.History},
		{"favorites", p.Favorites},
		{"following", p.Following},
		{"watchLater", p.WatchLater},
	} {
		b, err := c.load()
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		n := 0
		for _, sec := range b.Sections {
			n += len(sec.Videos)
		}
		if n == 0 {
			t.Errorf("%s: no videos", c.name)
		}
		t.Logf("%s: %d videos", c.name, n)
	}
}

// TestBilibiliDanmaku reads the comments of a video.
func TestBilibiliDanmaku(t *testing.T) {
	if os.Getenv("NETWORK") == "" {
		t.Skip("NETWORK is not set")
	}
	p, err := Load("../../examples/bilibili-plugin", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if c := os.Getenv("BILI_COOKIE"); c != "" {
		_ = p.SetSetting("cookie", c)
	}
	cues, err := p.Danmaku("BV1L6Wm69Eou#43525866910")
	if err != nil {
		t.Fatal(err)
	}
	if len(cues) == 0 {
		t.Fatal("no cues")
	}
	t.Logf("%d cues, first %q at %dms", len(cues), cues[0].Text, cues[0].Time)
}
