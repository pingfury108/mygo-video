// Package thumb takes a frame of a video into a JPEG, with the ffmpeg on
// the machine. Without one, videos keep the placeholder their name gives
// them.
package thumb

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"mygo-video/internal/executil"
	"mygo-video/internal/ffbin"
)

// Width is how wide a thumbnail is: enough for a card, a poster crop and a
// hero at the window's size.
const Width = 640

// Tool is the ffmpeg to take frames with, or "" when there is none.
func Tool() string {
	p, err := exec.LookPath("ffmpeg")
	if err != nil {
		return ""
	}
	return p
}

// Path is where the thumbnail of a video lives.
func Path(dir, id string) string {
	return filepath.Join(dir, "thumbs", id+".jpg")
}

// Make takes a frame of video into path, replacing what is there. It is
// taken a little way in, where a title sequence ends, and tried at the
// start for a clip too short for that. Call it off the main thread.
func Make(ctx context.Context, ffmpeg, video, path string) error {
	if ffmpeg == "" {
		return errors.New("thumb: no ffmpeg")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	var last error
	for _, at := range []string{"10", "1"} {
		last = run(ctx, ffmpeg, at, video, path)
		if last == nil {
			return nil
		}
	}
	return last
}

func run(ctx context.Context, ffmpeg, at, video, path string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, ffmpeg,
		"-y", "-loglevel", "error",
		"-rw_timeout", "15000000",
		"-ss", at,
		"-i", video,
		"-frames:v", "1",
		"-vf", "scale=640:-2",
		"-q:v", "4",
		path,
	)
	executil.Hide(cmd)
	if err := cmd.Run(); err != nil {
		_ = os.Remove(path)
		return err
	}
	if st, err := os.Stat(path); err != nil || st.Size() == 0 {
		_ = os.Remove(path)
		return errors.New("thumb: no frame")
	}
	return nil
}

// MakeURL takes a frame of a video at a URL, with the headers its server
// asks for, as a WebDAV one behind basic authentication does.
func MakeURL(ctx context.Context, ffmpeg, raw string, headers map[string]string, path string) error {
	if ffmpeg == "" {
		return errors.New("thumb: no ffmpeg")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	var last error
	for _, at := range []string{"10", "1"} {
		last = runURL(ctx, ffmpeg, at, raw, headers, path)
		if last == nil {
			return nil
		}
	}
	return last
}

func runURL(ctx context.Context, ffmpeg, at, raw string, headers map[string]string, path string) error {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	args := []string{"-y", "-loglevel", "error", "-rw_timeout", "15000000"}
	args = append(args, ffbin.NoProxyArgs(raw)...)
	if len(headers) > 0 {
		var b strings.Builder
		for k, v := range headers {
			b.WriteString(k)
			b.WriteString(": ")
			b.WriteString(v)
			b.WriteString("\r\n")
		}
		args = append(args, "-headers", b.String())
	}
	args = append(args,
		"-ss", at,
		"-i", raw,
		"-frames:v", "1",
		"-vf", "scale=640:-2",
		"-q:v", "4",
		path,
	)
	cmd := exec.CommandContext(ctx, ffmpeg, args...)
	executil.Hide(cmd)
	if err := cmd.Run(); err != nil {
		_ = os.Remove(path)
		return err
	}
	if st, err := os.Stat(path); err != nil || st.Size() == 0 {
		_ = os.Remove(path)
		return errors.New("thumb: no frame")
	}
	return nil
}
