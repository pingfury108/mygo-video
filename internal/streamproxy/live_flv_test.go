package streamproxy

import (
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestLiveFLV remuxes a live FLV stream, as a channel does, when LIVE_FLV
// holds a fresh address.
func TestLiveFLV(t *testing.T) {
	u := os.Getenv("LIVE_FLV")
	if u == "" {
		t.Skip("LIVE_FLV is not set")
	}
	ff, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("no ffmpeg")
	}
	p, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	url, token, err := p.Open(Stream{
		URL:  u,
		Type: "flv",
		Headers: map[string]string{
			"Referer":    "https://live.bilibili.com/",
			"User-Agent": os.Getenv("LIVE_UA"),
		},
		FFmpeg: ff,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Forget(token)
	deadline := time.Now().Add(30 * time.Second)
	start := time.Now()
	for {
		resp, err := http.Get(url)
		if err == nil {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK && strings.Contains(string(body), ".m4s") {
				t.Logf("playlist ready: %d parts in %v", strings.Count(string(body), ".m4s"), time.Since(start))
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("playlist did not come: %v", err)
		}
		time.Sleep(300 * time.Millisecond)
	}
}
