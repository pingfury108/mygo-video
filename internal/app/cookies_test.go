package app

import "testing"

func TestPluginCookieKept(t *testing.T) {
	a := &app{pluginDataDir: t.TempDir()}
	if err := a.savePluginCookie("bilibili", "SESSDATA=abc; buvid3=def"); err != nil {
		t.Fatal(err)
	}
	if got := a.readPluginCookie("bilibili"); got != "SESSDATA=abc; buvid3=def" {
		t.Fatalf("cookie = %q", got)
	}
	a.clearPluginCookie("bilibili")
	if got := a.readPluginCookie("bilibili"); got != "" {
		t.Fatalf("cookie after clear = %q", got)
	}
}

func TestCookieHostMatches(t *testing.T) {
	cases := []struct {
		domain string
		hosts  []string
		want   bool
	}{
		{".bilibili.com", []string{"bilibili.com"}, true},
		{"bilibili.com", []string{"bilibili.com"}, true},
		{"passport.bilibili.com", []string{"bilibili.com"}, true},
		{".example.com", []string{"bilibili.com"}, false},
		{".bilibili.com", []string{"*"}, true},
	}
	for _, c := range cases {
		if got := cookieHostMatches(c.domain, c.hosts); got != c.want {
			t.Errorf("cookieHostMatches(%q, %v) = %v", c.domain, c.hosts, got)
		}
	}
}
