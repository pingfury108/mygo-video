package streamproxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

// TestRelay plays a stream through a wrapper of the kind a collection
// site's own server is: the wrapper takes the source's address escaped in
// its path, and a playlist whose relative addresses the proxy has to keep
// resolving against the source, wrapping each one the same way.
func TestRelay(t *testing.T) {
	var calls int64
	up := upstream(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/master.m3u8":
			// A relative variant, which belongs to the source, not the
			// relay's own path.
			io.WriteString(w, "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=800000\nmedia.m3u8\n")
		case "/media.m3u8":
			io.WriteString(w, "#EXTM3U\n#EXT-X-TARGETDURATION:4\n#EXTINF:4,\nseg0.ts\n")
		case "/seg0.ts":
			io.WriteString(w, "SEGMENT0")
		default:
			http.NotFound(w, r)
		}
	})
	defer up.Close()

	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&calls, 1)
		if r.URL.Query().Get("auth") != "ok" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		raw, err := url.PathUnescape(strings.TrimPrefix(r.URL.Path, "/proxy/"))
		if err != nil || !strings.HasPrefix(raw, up.URL+"/") {
			http.Error(w, "bad target: "+r.URL.Path, http.StatusBadRequest)
			return
		}
		req, _ := http.NewRequest(http.MethodGet, raw, nil)
		// Whatever the app asked the source with goes on through, its
		// Referer among it.
		if ref := r.Header.Get("Referer"); ref != "" {
			req.Header.Set("Referer", ref)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
	}))
	defer relay.Close()

	p, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	entry, _, err := p.Open(Stream{
		URL:     up.URL + "/master.m3u8",
		Type:    "hls",
		Headers: map[string]string{"Referer": referer},
		Relay:   relay.URL + "/proxy/%s?auth=ok",
	})
	if err != nil {
		t.Fatal(err)
	}

	status, master := get(t, entry)
	if status != http.StatusOK {
		t.Fatalf("master status %d", status)
	}
	variants := lines(master)
	if len(variants) != 1 || !strings.HasPrefix(variants[0], p.Base()+"/s/") {
		t.Fatalf("variants %v", variants)
	}

	_, media := get(t, variants[0])
	segs := lines(media)
	if len(segs) != 1 || !strings.HasPrefix(segs[0], p.Base()+"/s/") {
		t.Fatalf("segments %v", segs)
	}
	_, body := get(t, segs[0])
	if body != "SEGMENT0" {
		t.Fatalf("segment %q", body)
	}

	// The playlist, the variant, and the segment all went through the
	// relay, which only passes targets that carry the auth pair.
	if n := atomic.LoadInt64(&calls); n < 3 {
		t.Fatalf("relay saw %d calls, want at least 3", n)
	}
}
