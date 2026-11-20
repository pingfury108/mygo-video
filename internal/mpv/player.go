// Package mpv plays a video in an mpv process of its own, drawn into this
// window: the system's media framework has no HEVC decoder, and a transcode
// would keep the CPU busy, while mpv decodes in hardware and draws the
// picture with the GPU.
package mpv

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"strconv"
	"sync"
	"time"

	"mygo-video/internal/executil"
)

// Player is an mpv process and the socket it answers on.
type Player struct {
	cmd    *exec.Cmd
	conn   io.ReadWriteCloser
	reveal func()

	mu      sync.Mutex
	next    int
	replies map[int]chan reply
}

type reply struct {
	RequestID int             `json:"request_id"`
	Error     string          `json:"error"`
	Data      json.RawMessage `json:"data"`
}

// launch starts mpv on url, drawn into wid, beginning at from, answering on
// the socket named by ipc.
func launch(bin, url string, from time.Duration, wid uintptr, ipc string) (*exec.Cmd, error) {
	args := []string{
		"--wid=" + strconv.FormatUint(uint64(wid), 10),
		ipc,
		"--no-config",
		"--osc=yes",
		"--really-quiet",
	}
	if from > 0 {
		args = append(args, "--start="+strconv.FormatFloat(from.Seconds(), 'f', 3, 64))
	}
	args = append(args, url)
	cmd := exec.Command(bin, args...)
	executil.Hide(cmd)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd, nil
}

// readLoop answers the calls by their request id.
func (p *Player) readLoop() {
	sc := bufio.NewScanner(p.conn)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		var r reply
		if json.Unmarshal(sc.Bytes(), &r) != nil {
			continue
		}
		p.mu.Lock()
		ch := p.replies[r.RequestID]
		delete(p.replies, r.RequestID)
		p.mu.Unlock()
		if ch != nil {
			ch <- r
		}
	}
}

// call sends a command and waits for its answer.
func (p *Player) call(within time.Duration, cmd ...any) (json.RawMessage, error) {
	p.mu.Lock()
	id := p.next
	p.next++
	ch := make(chan reply, 1)
	p.replies[id] = ch
	p.mu.Unlock()

	msg, err := json.Marshal(map[string]any{"command": cmd, "request_id": id})
	if err != nil {
		return nil, err
	}
	if _, err := p.conn.Write(append(msg, '\n')); err != nil {
		return nil, err
	}
	select {
	case r := <-ch:
		if r.Error != "success" {
			return nil, errors.New(r.Error)
		}
		return r.Data, nil
	case <-time.After(within):
		return nil, errors.New("mpv did not answer")
	}
}

// Position is where the video is, and zero when it is unknown.
func (p *Player) Position() time.Duration {
	data, err := p.call(2*time.Second, "get_property", "time-pos")
	if err != nil {
		return 0
	}
	var sec float64
	if json.Unmarshal(data, &sec) != nil {
		return 0
	}
	return time.Duration(sec * float64(time.Second))
}

// Pause stops and continues the video.
func (p *Player) Pause(v bool) {
	_, _ = p.call(time.Second, "set_property", "pause", v)
}

// Seek goes to a position.
func (p *Player) Seek(to time.Duration) {
	_, _ = p.call(time.Second, "seek", to.Seconds(), "absolute")
}

// Wait waits for mpv to end.
func (p *Player) Wait() error {
	if p.cmd == nil {
		return nil
	}
	return p.cmd.Wait()
}

// Stop ends mpv and gives the window back to the web view.
func (p *Player) Stop() {
	_, _ = p.call(time.Second, "quit")
	if p.cmd != nil && p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
	p.Reveal()
}

// Reveal shows the window's web view again, which Start hid.
func (p *Player) Reveal() {
	if p.reveal != nil {
		p.reveal()
	}
}
