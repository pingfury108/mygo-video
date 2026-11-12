package local

import "testing"

// Names a series names its episode by, as the releases and the sites do.
func TestParseName(t *testing.T) {
	cases := []struct {
		in      string
		title   string
		season  int
		episode int
	}{
		{"Fog Harbour S02E05.mkv", "Fog Harbour", 2, 5},
		{"Fog.Harbour.S02E05.1080p.WEB-DL.x264.mkv", "Fog Harbour", 2, 5},
		{"Show 1x05.mp4", "Show", 1, 5},
		{"雾港 第05集.mp4", "雾港", 0, 5},
		{"雾港.第5集.mp4", "雾港", 0, 5},
		{"Show E05.mp4", "Show", 0, 5},
		{"Show EP05.mp4", "Show", 0, 5},
		{"山雨.mp4", "山雨", 0, 0},
		{"Heat.1995.1080p.BluRay.mkv", "Heat 1995", 0, 0},
	}
	for _, c := range cases {
		title, season, episode := ParseName(c.in)
		if title != c.title || season != c.season || episode != c.episode {
			t.Errorf("ParseName(%q) = %q %d %d, want %q %d %d",
				c.in, title, season, episode, c.title, c.season, c.episode)
		}
	}
}
