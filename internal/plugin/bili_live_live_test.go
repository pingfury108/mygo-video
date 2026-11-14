package plugin

import (
	"os"
	"sync"
	"testing"
	"time"
)

// TestBilibiliLive reads the channels a site is showing, plays one, and
// takes its chat for a few seconds.
func TestBilibiliLive(t *testing.T) {
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
	b, err := p.LiveList()
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Sections) == 0 || len(b.Sections[0].Videos) == 0 {
		t.Fatal("no channels")
	}
	first := b.Sections[0].Videos[0]
	if !first.Live {
		t.Fatal("channel is not live")
	}
	t.Logf("channel: %s (%s)", first.Name, first.ID)
	pb, err := p.Play(first.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if !pb.Live || pb.URL == "" {
		t.Fatalf("play = %+v", pb)
	}
	t.Logf("stream: %s %s", pb.Type, pb.URL[:min(70, len(pb.URL))])

	var mu sync.Mutex
	cues := 0
	p.SetDanmakuHandler(func(Cue) {
		mu.Lock()
		cues++
		mu.Unlock()
	})
	if err := p.LiveDanmaku(first.ID); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := cues
		mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	_ = p.StopLive()
	mu.Lock()
	n := cues
	mu.Unlock()
	if n == 0 {
		t.Fatal("no chat")
	}
	t.Logf("chat: %d comments", n)
}
