package iptv

import (
	"os"
	"testing"
)

// TestLivePlaylist reads a real playlist when NETWORK=1.
func TestLivePlaylist(t *testing.T) {
	if os.Getenv("NETWORK") == "" {
		t.Skip("NETWORK is not set")
	}
	chs, err := Load("https://iptv-org.github.io/iptv/countries/cn.m3u")
	if err != nil {
		t.Fatal(err)
	}
	if len(chs) == 0 {
		t.Fatal("no channels")
	}
	t.Logf("%d channels, first %q in %q", len(chs), chs[0].Name, chs[0].Group)
}
