// Package streamproxy serves a stream to the system's media framework the
// way its source expects it: with the headers it needs, over a loopback
// address of this app's own, and with HLS playlists rewritten so that their
// parts come back through it too. Segments encrypted with AES-128 are
// decrypted in memory, so the player sees plain MPEG-TS or fMP4.
//
// MyGo's video players, as the system frameworks under them, take a URL and
// no headers; a source, which may keep its streams behind a Referer, a
// Cookie or a signed query, therefore cannot hand one over as it is.
package streamproxy

import (
	"bufio"
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"mygo-video/internal/executil"
)

// Stream is what a source hands over to play.
type Stream struct {
	// URL is the stream's own address: an HLS playlist, or a file.
	URL string
	// Type is "hls" or "mp4". Empty asks for HLS when the URL ends in
	// .m3u8, and a file otherwise.
	Type string
	// Headers go with every request to the source: Referer, User-Agent,
	// Cookie, Origin, and the like.
	Headers map[string]string
	// FFmpeg is the program that remuxes an MP4 whose moov is at its end
	// into HLS, which the player streams; empty leaves such files to it,
	// which waits for the whole of them.
	FFmpeg string
	// Start is where the stream begins, for a seek that restarts the
	// remux at the place asked for.
	Start time.Duration
	// Audio is where the sound of a stream kept apart comes from, as DASH
	// does; the remux joins it to the picture.
	Audio string
	// VideoEncoder is what a transcode encodes the picture with, such as
	// h264_nvenc; empty uses libx264.
	VideoEncoder string

	// hls is the remux running for this stream, if any.
	hls *hlsJob
}

// hlsJob is a remux writing HLS parts into a folder of its own.
type hlsJob struct {
	dir    string
	cancel context.CancelFunc
	cmd    *exec.Cmd
}

// Proxy hands streams to the player over loopback.
type Proxy struct {
	ln     net.Listener
	srv    *http.Server
	base   string
	client *http.Client

	mu      sync.Mutex
	streams map[string]*Stream
	keys    map[string][]byte
	// faststart says whether an MP4's moov is at its start, by URL.
	faststart map[string]bool
}

// New starts a proxy on a loopback port of its own.
func New() (*Proxy, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	p := &Proxy{
		ln:        ln,
		base:      "http://" + ln.Addr().String(),
		client:    &http.Client{Transport: &http.Transport{DisableCompression: true}},
		streams:   map[string]*Stream{},
		keys:      map[string][]byte{},
		faststart: map[string]bool{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/s/", p.serve)
	p.srv = &http.Server{Handler: mux}
	go p.srv.Serve(ln)
	return p, nil
}

// Base is the loopback address streams are served on.
func (p *Proxy) Base() string { return p.base }

// Close stops the proxy and every stream it serves.
func (p *Proxy) Close() error {
	p.mu.Lock()
	streams := p.streams
	p.streams = map[string]*Stream{}
	p.mu.Unlock()
	for _, s := range streams {
		stopHLS(s)
	}
	return p.srv.Close()
}

// Open starts serving s and answers the URL to play, and the token to stop
// it with: its playlist for HLS, or the file itself.
func (p *Proxy) Open(s Stream) (string, string, error) {
	if s.URL == "" {
		return "", "", errors.New("streamproxy: no URL")
	}
	if s.Type == "" {
		s.Type = "mp4"
		if strings.Contains(strings.ToLower(s.URL), ".m3u8") {
			s.Type = "hls"
		}
	}
	token, err := newToken()
	if err != nil {
		return "", "", err
	}

	// An MP4 with its moov at the end can't be streamed by the system's
	// player over HTTP: it waits for the whole file. Remux it to HLS as it
	// comes, which the player begins on at once.
	if s.Audio != "" || s.Type == "dash" || s.Type == "flv" || s.Type == "remux" || s.Type == "transcode" {
		if s.FFmpeg == "" {
			return "", "", errors.New("streamproxy: this stream needs ffmpeg")
		}
		job, err := p.startHLS(&s)
		if err != nil {
			return "", "", err
		}
		s.hls = job
		p.mu.Lock()
		p.streams[token] = &s
		p.mu.Unlock()
		return p.base + "/s/" + token + "/hls/index.m3u8", token, nil
	}
	if s.Type == "mp4" && s.FFmpeg != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		fast := p.isFaststart(ctx, &s, s.URL)
		cancel()
		if !fast {
			if job, err := p.startHLS(&s); err == nil {
				s.hls = job
				p.mu.Lock()
				p.streams[token] = &s
				p.mu.Unlock()
				return p.base + "/s/" + token + "/hls/index.m3u8", token, nil
			}
		}
	}

	p.mu.Lock()
	p.streams[token] = &s
	p.mu.Unlock()

	kind := "b"
	if s.Type == "hls" {
		kind = "p"
	}
	return p.base + "/s/" + token + "/" + kind + "/" + encode(s.URL), token, nil
}

// Forget stops serving a stream.
func (p *Proxy) Forget(token string) {
	p.mu.Lock()
	s := p.streams[token]
	delete(p.streams, token)
	p.mu.Unlock()
	stopHLS(s)
}

// Log is where the proxy reports what ffmpeg said. The app points it at its
// diagnostic log, which a windowed program can read after the fact.
var Log = func(msg string) { log.Printf("streamproxy: %s", msg) }

func stopHLS(s *Stream) {
	if s == nil || s.hls == nil {
		return
	}
	s.hls.cancel()
	_ = s.hls.cmd.Wait()
	_ = os.RemoveAll(s.hls.dir)
}

func (p *Proxy) stream(token string) *Stream {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.streams[token]
}

func (p *Proxy) serve(w http.ResponseWriter, r *http.Request) {
	parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/s/"), "/", 3)
	if len(parts) != 3 {
		http.NotFound(w, r)
		return
	}
	token, kind, enc := parts[0], parts[1], parts[2]
	s := p.stream(token)
	if s == nil {
		http.NotFound(w, r)
		return
	}
	if kind == "hls" {
		p.serveHLS(w, r, s, enc)
		return
	}
	raw, err := decode(enc)
	if err != nil {
		http.Error(w, "bad stream address", http.StatusBadRequest)
		return
	}
	switch kind {
	case "p":
		p.servePlaylist(w, r, s, raw, token)
	case "b":
		p.serveBytes(w, r, s, raw)
	case "k":
		p.serveKey(w, r, s, raw)
	default:
		http.NotFound(w, r)
	}
}

func (p *Proxy) servePlaylist(w http.ResponseWriter, r *http.Request, s *Stream, raw, token string) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	resp, err := p.fetch(ctx, s, raw, "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		http.Error(w, resp.Status, resp.StatusCode)
		return
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	// A master playlist, or a media one: both are rewritten. Anything else
	// a source calls a playlist is passed through as it is.
	if !strings.Contains(string(body), "#EXTM3U") {
		copyHeader(w, resp, "Content-Type")
		w.Write(body)
		return
	}
	base := resp.Request.URL
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Write(p.rewrite(body, base, token))
}

func (p *Proxy) serveBytes(w http.ResponseWriter, r *http.Request, s *Stream, raw string) {
	key := r.URL.Query().Get("k")
	iv := r.URL.Query().Get("iv")
	seq, _ := strconv.ParseUint(r.URL.Query().Get("seq"), 10, 64)

	// A file goes on for as long as the player reads it: its own request
	// goes away when the player closes the connection.
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	rng := r.Header.Get("Range")
	// A decrypted segment is served whole: its length is only known once it
	// came, and the player may ask for a part of the plain one.
	if key != "" {
		rng = ""
	}
	method := http.MethodGet
	if r.Method == http.MethodHead {
		method = http.MethodHead
	}
	resp, err := p.fetchMethod(ctx, s, raw, rng, method)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		http.Error(w, resp.Status, resp.StatusCode)
		return
	}
	if r.Method == http.MethodHead {
		copyHeader(w, resp, "Content-Type")
		copyHeader(w, resp, "Content-Length")
		copyHeader(w, resp, "Content-Range")
		copyHeader(w, resp, "Accept-Ranges")
		w.WriteHeader(resp.StatusCode)
		return
	}

	// An MP4 with its moov at the end is HLS here, from Open.

	if key != "" {
		data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		plain, err := p.decrypt(ctx, s, key, parseIV(iv, seq), data)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		copyHeader(w, resp, "Content-Type")
		w.Header().Set("Content-Length", strconv.Itoa(len(plain)))
		w.WriteHeader(http.StatusOK)
		w.Write(plain)
		return
	}

	copyHeader(w, resp, "Content-Type")
	copyHeader(w, resp, "Content-Length")
	copyHeader(w, resp, "Content-Range")
	copyHeader(w, resp, "Accept-Ranges")
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func (p *Proxy) serveKey(w http.ResponseWriter, r *http.Request, s *Stream, raw string) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	key, err := p.key(ctx, s, raw)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Write(key)
}

// isFaststart says whether an MP4's moov comes before its mdat, which the
// system's player needs to stream it over HTTP.
func (p *Proxy) isFaststart(ctx context.Context, s *Stream, raw string) bool {
	p.mu.Lock()
	v, ok := p.faststart[raw]
	p.mu.Unlock()
	if ok {
		return v
	}
	resp, err := p.fetchMethod(ctx, s, raw, "bytes=0-131071", http.MethodGet)
	if err != nil {
		return true
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return true
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<17))
	if err != nil {
		return true
	}
	v = moovBeforeMdat(data)
	p.mu.Lock()
	p.faststart[raw] = v
	p.mu.Unlock()
	return v
}

// moovBeforeMdat says whether the moov comes before the mdat, as a file the
// player can stream over HTTP has it.
func moovBeforeMdat(data []byte) bool {
	moov := bytes.Index(data, []byte("moov"))
	if moov < 0 {
		return false
	}
	mdat := bytes.Index(data, []byte("mdat"))
	if mdat < 0 {
		return true
	}
	return moov < mdat
}

// startHLS makes the file's parts as HLS, which the player streams from a
// playlist that grows as they come.
// TempDir is where HLS parts are written, an empty string for the system's
// temporary directory. The app points it at its data directory, so the drive
// the listener chose holds them too.
var TempDir string

func tempRoot() string {
	if TempDir != "" {
		return TempDir
	}
	return os.TempDir()
}

func (p *Proxy) startHLS(s *Stream) (*hlsJob, error) {
	dir, err := os.MkdirTemp(tempRoot(), "mygo-video-hls-")
	if err != nil {
		return nil, err
	}
	args := []string{"-loglevel", "error", "-rw_timeout", "15000000"}
	if s.Start > 0 {
		args = append(args, "-ss", fmt.Sprintf("%.3f", s.Start.Seconds()))
	}
	// Each input takes the headers of its own request.
	// A live stream starts at once: no probing pause, no input buffer,
	// and shorter parts, so the player has one to begin on.
	live := s.Type == "flv"
	if live {
		args = append(args, "-fflags", "nobuffer", "-analyzeduration", "0", "-probesize", "524288")
	}
	// A file the system's player has no decoder for is transcoded.
	transcode := s.Type == "transcode"
	if transcode {
		// The GPU decodes what the player cannot, which is far faster than
		// the CPU.
		args = append(args, "-hwaccel", "auto")
	}
	addInput := func(u string) {
		if h := headerLines(s.Headers); h != "" {
			args = append(args, "-headers", h)
		}
		args = append(args, "-i", u)
	}
	addInput(s.URL)
	if s.Audio != "" {
		addInput(s.Audio)
		args = append(args, "-map", "0:v:0", "-map", "1:a:0")
	}
	hlsTime := "4"
	if live || transcode {
		hlsTime = "2"
	}
	if transcode {
		args = append(args, encoderArgs(s.VideoEncoder)...)
		args = append(args, "-c:a", "aac", "-b:a", "128k")
	} else {
		args = append(args, "-c", "copy")
	}
	args = append(args,
		"-f", "hls",
		"-hls_time", hlsTime,
		"-hls_playlist_type", "event",
		// MPEG-TS rather than fMP4: Media Foundation plays HLS of TS
		// segments, not of fMP4 ones.
		"-hls_segment_type", "mpegts",
		"-hls_segment_filename", filepath.Join(dir, "seg%05d.ts"),
		"-hls_flags", "independent_segments",
		filepath.Join(dir, "index.m3u8"),
	)
	ctx, cancel := context.WithCancel(context.Background())
	job := &hlsJob{dir: dir, cancel: cancel}
	cmd := exec.CommandContext(ctx, s.FFmpeg, args...)
	executil.Hide(cmd)
	job.cmd = cmd
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		_ = os.RemoveAll(dir)
		return nil, err
	}
	Log("hls " + strings.Join(args, " "))
	if err := cmd.Start(); err != nil {
		cancel()
		_ = os.RemoveAll(dir)
		return nil, err
	}
	// What ffmpeg says goes to the log as it comes, not only at the end:
	// a file it cannot decode says why there.
	go func() {
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			if line := strings.TrimSpace(sc.Text()); line != "" {
				Log("ffmpeg: " + line)
			}
		}
	}()
	return job, nil
}

// serveHLS serves a part of the remux, waiting a while for the playlist to
// be written before the player asks for it.
func (p *Proxy) serveHLS(w http.ResponseWriter, r *http.Request, s *Stream, name string) {
	if s.hls == nil {
		http.NotFound(w, r)
		return
	}
	name = filepath.Base(name)
	path := filepath.Join(s.hls.dir, name)
	if strings.HasSuffix(name, ".m3u8") {
		// The first part comes once ffmpeg has read the few seconds it
		// holds; a player that waits here longer than that is better off
		// with an error, which makes the app try the next way in.
		deadline := time.Now().Add(8 * time.Second)
		for {
			if _, err := os.Stat(path); err == nil {
				break
			}
			if time.Now().After(deadline) {
				http.Error(w, "the playlist isn't ready", http.StatusGatewayTimeout)
				return
			}
			select {
			case <-r.Context().Done():
				return
			case <-time.After(200 * time.Millisecond):
			}
		}
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	} else if strings.HasSuffix(name, ".ts") {
		// Media Foundation reads the type of a part, and turns down a TS
		// part sent as video/mp4.
		w.Header().Set("Content-Type", "video/mp2t")
	} else {
		w.Header().Set("Content-Type", "video/mp4")
	}
	http.ServeFile(w, r, path)
}

// encoderArgs is how a transcode encodes its picture: the GPU's encoder where
// there is one, which keeps up with a 4K picture, else x264.
func encoderArgs(enc string) []string {
	switch enc {
	case "h264_nvenc":
		return []string{"-c:v", "h264_nvenc", "-preset", "p4", "-rc", "vbr", "-cq", "23", "-b:v", "0"}
	case "h264_qsv":
		return []string{"-c:v", "h264_qsv", "-preset", "veryfast", "-global_quality", "23"}
	case "h264_amf":
		return []string{"-c:v", "h264_amf", "-quality", "speed", "-rc", "cqp", "-qp_i", "23", "-qp_p", "23"}
	case "h264_mf":
		return []string{"-c:v", "h264_mf", "-b:v", "12M"}
	}
	return []string{"-c:v", "libx264", "-preset", "veryfast", "-crf", "23"}
}

func headerLines(h map[string]string) string {
	if len(h) == 0 {
		return ""
	}
	var b strings.Builder
	for k, v := range h {
		b.WriteString(k)
		b.WriteString(": ")
		b.WriteString(v)
		b.WriteString("\r\n")
	}
	return b.String()
}

func (p *Proxy) fetch(ctx context.Context, s *Stream, raw, rng string) (*http.Response, error) {
	return p.fetchMethod(ctx, s, raw, rng, http.MethodGet)
}

func (p *Proxy) fetchMethod(ctx context.Context, s *Stream, raw, rng, method string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, raw, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range s.Headers {
		req.Header.Set(k, v)
	}
	if rng != "" {
		req.Header.Set("Range", rng)
	}
	return p.client.Do(req)
}

func (p *Proxy) key(ctx context.Context, s *Stream, raw string) ([]byte, error) {
	p.mu.Lock()
	key, ok := p.keys[raw]
	p.mu.Unlock()
	if ok {
		return key, nil
	}
	resp, err := p.fetch(ctx, s, raw, "")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("streamproxy: key: %s", resp.Status)
	}
	key, err = io.ReadAll(io.LimitReader(resp.Body, 64))
	if err != nil {
		return nil, err
	}
	if len(key) != 16 && len(key) != 24 && len(key) != 32 {
		return nil, errors.New("streamproxy: key is not 16, 24 or 32 bytes")
	}
	p.mu.Lock()
	p.keys[raw] = key
	p.mu.Unlock()
	return key, nil
}

func (p *Proxy) decrypt(ctx context.Context, s *Stream, keyURL string, iv []byte, data []byte) ([]byte, error) {
	key, err := p.key(ctx, s, keyURL)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || len(data)%aes.BlockSize != 0 {
		return nil, errors.New("streamproxy: segment is not a whole number of blocks")
	}
	plain := make([]byte, len(data))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(plain, data)
	// PKCS#7 padding, which HLS uses.
	if n := int(plain[len(plain)-1]); n > 0 && n <= aes.BlockSize && n <= len(plain) {
		for _, b := range plain[len(plain)-n:] {
			if int(b) != n {
				return plain, nil
			}
		}
		plain = plain[:len(plain)-n]
	}
	return plain, nil
}

// rewrite turns a playlist's addresses into the proxy's own, keeping the
// key each segment is encrypted with.
func (p *Proxy) rewrite(body []byte, base *url.URL, token string) []byte {
	lines := strings.Split(strings.ReplaceAll(string(body), "\r\n", "\n"), "\n")
	out := make([]string, 0, len(lines))
	var keyURL string
	var keyIV string
	var seq uint64
	variant := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "#EXT-X-MEDIA-SEQUENCE:"):
			seq, _ = strconv.ParseUint(strings.TrimSpace(strings.TrimPrefix(trimmed, "#EXT-X-MEDIA-SEQUENCE:")), 10, 64)
			out = append(out, line)
		case strings.HasPrefix(trimmed, "#EXT-X-KEY:"):
			attrs := parseAttrs(trimmed)
			method := strings.ToUpper(attrs["METHOD"])
			if method == "AES-128" && attrs["URI"] != "" {
				keyURL = resolve(base, attrs["URI"]).String()
				keyIV = attrs["IV"]
				attrs["URI"] = p.base + "/s/" + token + "/k/" + encode(keyURL)
				out = append(out, rebuildTag("EXT-X-KEY", attrs))
			} else {
				keyURL, keyIV = "", ""
				out = append(out, line)
			}
		case strings.HasPrefix(trimmed, "#EXT-X-MAP:"):
			out = append(out, p.rewriteTagURI(trimmed, base, token, "b", keyURL, keyIV, 0))
		case strings.HasPrefix(trimmed, "#EXT-X-MEDIA:"),
			strings.HasPrefix(trimmed, "#EXT-X-I-FRAME-STREAM-INF:"):
			out = append(out, p.rewriteTagURI(trimmed, base, token, "p", "", "", 0))
		case strings.HasPrefix(trimmed, "#EXT-X-STREAM-INF"):
			out = append(out, line)
			variant = true
		case trimmed == "" || strings.HasPrefix(trimmed, "#"):
			out = append(out, line)
		default:
			u := resolve(base, trimmed)
			if variant {
				out = append(out, p.playlistURL(token, u))
				variant = false
			} else {
				out = append(out, p.segmentURL(token, u, keyURL, keyIV, seq))
				seq++
			}
		}
	}
	return []byte(strings.Join(out, "\n"))
}

func (p *Proxy) playlistURL(token string, u *url.URL) string {
	return p.base + "/s/" + token + "/p/" + encode(u.String())
}

func (p *Proxy) segmentURL(token string, u *url.URL, keyURL, keyIV string, seq uint64) string {
	q := url.Values{}
	if keyURL != "" {
		q.Set("k", keyURL)
		if keyIV != "" {
			q.Set("iv", keyIV)
		}
		q.Set("seq", strconv.FormatUint(seq, 10))
	}
	out := p.base + "/s/" + token + "/b/" + encode(u.String())
	if len(q) > 0 {
		out += "?" + q.Encode()
	}
	return out
}

// rewriteTagURI rewrites the URI attribute of a tag, as EXT-X-MAP or
// EXT-X-MEDIA has it.
func (p *Proxy) rewriteTagURI(line string, base *url.URL, token, kind, keyURL, keyIV string, seq uint64) string {
	name, attrs := nameAndAttrs(line)
	uri := attrs["URI"]
	if uri == "" {
		return line
	}
	u := resolve(base, uri)
	if kind == "p" {
		attrs["URI"] = p.playlistURL(token, u)
	} else {
		attrs["URI"] = p.segmentURL(token, u, keyURL, keyIV, seq)
	}
	return rebuildTag(name, attrs)
}

func resolve(base *url.URL, ref string) *url.URL {
	u, err := url.Parse(ref)
	if err != nil {
		return base
	}
	return base.ResolveReference(u)
}

func parseIV(s string, seq uint64) []byte {
	s = strings.TrimSpace(s)
	if s != "" {
		if b, err := hex.DecodeString(strings.TrimPrefix(strings.TrimPrefix(s, "0x"), "0X")); err == nil && len(b) == aes.BlockSize {
			return b
		}
	}
	iv := make([]byte, aes.BlockSize)
	binary.BigEndian.PutUint64(iv[8:], seq)
	return iv
}

// parseAttrs reads a tag's attributes: METHOD=AES-128,URI="key.bin".
func parseAttrs(line string) map[string]string {
	_, attrs := nameAndAttrs(line)
	return attrs
}

// nameAndAttrs splits a tag at its colon, then its attributes, keeping the
// ones a value's comma does not end.
func nameAndAttrs(line string) (string, map[string]string) {
	name := strings.TrimPrefix(strings.TrimSpace(line), "#")
	rest := ""
	if i := strings.IndexByte(name, ':'); i >= 0 {
		name, rest = name[:i], name[i+1:]
	}
	attrs := map[string]string{}
	for len(rest) > 0 {
		rest = strings.TrimLeft(rest, " \t,")
		if rest == "" {
			break
		}
		eq := strings.IndexByte(rest, '=')
		if eq < 0 {
			break
		}
		k := strings.TrimSpace(rest[:eq])
		rest = rest[eq+1:]
		var v string
		if strings.HasPrefix(rest, `"`) {
			if end := strings.IndexByte(rest[1:], '"'); end >= 0 {
				v = rest[1 : 1+end]
				rest = rest[2+end:]
			} else {
				v = rest[1:]
				rest = ""
			}
		} else {
			if comma := strings.IndexByte(rest, ','); comma >= 0 {
				v = rest[:comma]
				rest = rest[comma+1:]
			} else {
				v = rest
				rest = ""
			}
		}
		attrs[k] = v
	}
	return name, attrs
}

func rebuildTag(name string, attrs map[string]string) string {
	// The order of a map is not stable, and a playlist reader does not care;
	// URI goes first as it reads best.
	var b strings.Builder
	b.WriteString("#")
	b.WriteString(name)
	b.WriteString(":")
	if uri, ok := attrs["URI"]; ok {
		fmt.Fprintf(&b, `URI="%s"`, uri)
		delete(attrs, "URI")
		for k, v := range attrs {
			fmt.Fprintf(&b, `,%s=%s`, k, quoteAttr(v))
		}
	} else {
		first := true
		for k, v := range attrs {
			if !first {
				b.WriteString(",")
			}
			first = false
			fmt.Fprintf(&b, `%s=%s`, k, quoteAttr(v))
		}
	}
	return b.String()
}

func quoteAttr(v string) string {
	if v == "" {
		return `""`
	}
	if strings.ContainsAny(v, `, "`) {
		return `"` + strings.ReplaceAll(v, `"`, `\"`) + `"`
	}
	return v
}

func copyHeader(w http.ResponseWriter, resp *http.Response, name string) {
	if v := resp.Header.Get(name); v != "" {
		w.Header().Set(name, v)
	}
}

func newToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func encode(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }

func decode(s string) (string, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
