package streamproxy

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const referer = "https://example.com/watch"

// upstream is a server that keeps its streams behind a Referer, as the
// sites the proxy is for do.
func upstream(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Referer") != referer {
			http.Error(w, "no referer", http.StatusForbidden)
			return
		}
		h(w, r)
	}))
}

func get(t *testing.T, u string) (int, string) {
	t.Helper()
	resp, err := http.Get(u)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, string(body)
}

func lines(body string) []string {
	var out []string
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			out = append(out, line)
		}
	}
	return out
}

// A playlist comes back through the proxy, its parts rewritten, and the
// segment itself is fetched with the headers the source wants.
func TestHLSPlaylist(t *testing.T) {
	up := upstream(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/master.m3u8":
			io.WriteString(w, "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=800000\nmedia.m3u8\n")
		case "/media.m3u8":
			io.WriteString(w, "#EXTM3U\n#EXT-X-TARGETDURATION:4\n#EXTINF:4,\nseg0.ts\n#EXTINF:4,\nseg1.ts\n")
		case "/seg0.ts":
			io.WriteString(w, "SEGMENT0")
		default:
			http.NotFound(w, r)
		}
	})
	defer up.Close()

	p, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	entry, _, err := p.Open(Stream{URL: up.URL + "/master.m3u8", Type: "hls", Headers: map[string]string{"Referer": referer}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(entry, p.Base()+"/s/") {
		t.Fatalf("entry %q", entry)
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
	if len(segs) != 2 || !strings.HasPrefix(segs[0], p.Base()+"/s/") {
		t.Fatalf("segments %v", segs)
	}
	_, body := get(t, segs[0])
	if body != "SEGMENT0" {
		t.Fatalf("segment %q", body)
	}
}

// AES-128 segments are decrypted in memory, and the player gets plain bytes.
func TestHLSEncrypted(t *testing.T) {
	key := []byte("0123456789abcdef")
	iv := []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}
	plain := []byte("hello encrypted segment, padded to a block")
	enc := encrypt(t, key, iv, plain)

	up := upstream(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/media.m3u8":
			io.WriteString(w, "#EXTM3U\n#EXT-X-TARGETDURATION:4\n"+
				"#EXT-X-KEY:METHOD=AES-128,URI=\"key.bin\",IV=0x000102030405060708090a0b0c0d0e0f\n"+
				"#EXTINF:4,\nseg0.ts\n")
		case "/key.bin":
			w.Write(key)
		case "/seg0.ts":
			w.Write(enc)
		default:
			http.NotFound(w, r)
		}
	})
	defer up.Close()

	p, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	entry, _, err := p.Open(Stream{URL: up.URL + "/media.m3u8", Type: "hls", Headers: map[string]string{"Referer": referer}})
	if err != nil {
		t.Fatal(err)
	}
	_, media := get(t, entry)
	if !strings.Contains(media, "METHOD=AES-128") {
		t.Fatalf("key tag not kept: %s", media)
	}
	if !strings.Contains(media, "/k/") {
		t.Fatalf("key URI not rewritten: %s", media)
	}
	segs := lines(media)
	if len(segs) != 1 {
		t.Fatalf("segments %v", segs)
	}
	_, body := get(t, segs[0])
	if body != string(plain) {
		t.Fatalf("segment %q, want %q", body, plain)
	}
}

// A file is passed through with its Range, which seeking in an MP4 needs.
func TestFileRange(t *testing.T) {
	up := upstream(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/movie.mp4" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Range") != "bytes=10-19" {
			http.Error(w, "no range", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Range", "bytes 10-19/100")
		w.Header().Set("Content-Length", "10")
		w.WriteHeader(http.StatusPartialContent)
		io.WriteString(w, "0123456789")
	})
	defer up.Close()

	p, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	u, _, err := p.Open(Stream{URL: up.URL + "/movie.mp4", Type: "mp4", Headers: map[string]string{"Referer": referer}})
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodGet, u, nil)
	req.Header.Set("Range", "bytes=10-19")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if resp.Header.Get("Content-Range") != "bytes 10-19/100" {
		t.Fatalf("content range %q", resp.Header.Get("Content-Range"))
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "0123456789" {
		t.Fatalf("body %q", body)
	}
}

// Without its Referer the source turns the proxy away, and so does the
// proxy.
func TestMissingHeaders(t *testing.T) {
	up := upstream(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "#EXTM3U\n")
	})
	defer up.Close()

	p, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	entry, _, err := p.Open(Stream{URL: up.URL + "/media.m3u8", Type: "hls"})
	if err != nil {
		t.Fatal(err)
	}
	status, _ := get(t, entry)
	if status != http.StatusForbidden {
		t.Fatalf("status %d, want 403", status)
	}
}

// A stream stops when it is forgotten.
func TestForget(t *testing.T) {
	up := upstream(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "#EXTM3U\n")
	})
	defer up.Close()
	p, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	entry, _, err := p.Open(Stream{URL: up.URL + "/media.m3u8", Type: "hls", Headers: map[string]string{"Referer": referer}})
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(strings.TrimPrefix(entry, p.Base()+"/s/"), "/")
	if len(parts) == 0 {
		t.Fatal("no token")
	}
	p.Forget(parts[0])
	status, _ := get(t, entry)
	if status != http.StatusNotFound {
		t.Fatalf("status %d, want 404", status)
	}
}

func encrypt(t *testing.T, key, iv, plain []byte) []byte {
	t.Helper()
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	pad := aes.BlockSize - len(plain)%aes.BlockSize
	padded := append(append([]byte(nil), plain...), bytes.Repeat([]byte{byte(pad)}, pad)...)
	out := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(out, padded)
	return out
}

// An MP4 whose moov is at its end is remuxed to HLS that the player can
// stream from.
func TestHLSRemux(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("no ffmpeg")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "clip.mp4")
	cmd := exec.Command(ffmpeg, "-y", "-loglevel", "error",
		"-f", "lavfi", "-i", "testsrc=duration=2:size=320x240:rate=25",
		"-f", "lavfi", "-i", "sine=duration=2",
		"-c:v", "libx264", "-c:a", "aac", "-shortest", src)
	if err := cmd.Run(); err != nil {
		t.Skipf("can't make a clip: %v", err)
	}
	srv := httptest.NewServer(http.FileServer(http.Dir(dir)))
	defer srv.Close()

	p, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	u, _, err := p.Open(Stream{URL: srv.URL + "/clip.mp4", Type: "mp4", FFmpeg: ffmpeg})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(u, "/hls/index.m3u8") {
		t.Fatalf("url %q, want HLS", u)
	}
	deadline := time.Now().Add(25 * time.Second)
	for {
		resp, err := http.Get(u)
		if err == nil {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK && strings.Contains(string(body), "#EXTM3U") {
				if !strings.Contains(string(body), ".m4s") {
					t.Fatalf("no parts: %s", body)
				}
				base := strings.TrimSuffix(u, "index.m3u8")
				init, err := http.Get(base + "init.mp4")
				if err != nil {
					t.Fatal(err)
				}
				init.Body.Close()
				if init.StatusCode != http.StatusOK {
					t.Fatalf("init %d", init.StatusCode)
				}
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("playlist did not come")
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// A stream kept apart, as DASH has it, is joined into HLS: both the
// picture and its sound come out.
func TestHLSRemuxDash(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("no ffmpeg")
	}
	dir := t.TempDir()
	video := filepath.Join(dir, "v.mp4")
	audio := filepath.Join(dir, "a.m4a")
	mk := func(args ...string) {
		t.Helper()
		cmd := exec.Command(ffmpeg, append([]string{"-y", "-loglevel", "error"}, args...)...)
		if err := cmd.Run(); err != nil {
			t.Skipf("can't make a clip: %v", err)
		}
	}
	mk("-f", "lavfi", "-i", "testsrc=duration=2:size=320x240:rate=25", "-c:v", "libx264", "-an", video)
	mk("-f", "lavfi", "-i", "sine=duration=2", "-c:a", "aac", audio)
	srv := httptest.NewServer(http.FileServer(http.Dir(dir)))
	defer srv.Close()

	p, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	u, _, err := p.Open(Stream{
		URL:    srv.URL + "/v.mp4",
		Audio:  srv.URL + "/a.m4a",
		Type:   "dash",
		FFmpeg: ffmpeg,
	})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(25 * time.Second)
	for {
		resp, err := http.Get(u)
		if err == nil {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK && strings.Contains(string(body), ".m4s") {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("playlist did not come")
		}
		time.Sleep(200 * time.Millisecond)
	}

	probe, err := exec.LookPath("ffprobe")
	if err != nil {
		return
	}
	out, err := exec.Command(probe, "-v", "error", "-show_entries", "stream=codec_type",
		"-of", "csv=p=0", u).Output()
	if err != nil {
		t.Fatalf("ffprobe: %v", err)
	}
	if !strings.Contains(string(out), "video") || !strings.Contains(string(out), "audio") {
		t.Fatalf("streams: %s", out)
	}
}
