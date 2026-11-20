//go:build darwin

package mpv

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"
)

// Start runs mpv on url, drawn into wid, starting at from. On macOS the web
// view is left as it is: hiding it needs an Objective-C call this package
// does not make yet, and the platform is not where the app ships mpv.
func Start(bin, url string, from time.Duration, wid uintptr) (*Player, error) {
	sock := filepath.Join(os.TempDir(), fmt.Sprintf("mygo-video-mpv-%d.sock", os.Getpid()))
	_ = os.Remove(sock)
	cmd, err := launch(bin, url, from, wid, "--input-ipc-server="+sock)
	if err != nil {
		return nil, err
	}
	p := &Player{cmd: cmd, replies: map[int]chan reply{}}
	conn, err := waitSocket(sock, 5*time.Second)
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, err
	}
	p.conn = conn
	go p.readLoop()
	return p, nil
}

// waitSocket waits for mpv to open its socket: it takes a moment after start.
func waitSocket(path string, within time.Duration) (net.Conn, error) {
	deadline := time.Now().Add(within)
	var err error
	for time.Now().Before(deadline) {
		var conn net.Conn
		conn, err = net.Dial("unix", path)
		if err == nil {
			return conn, nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return nil, fmt.Errorf("mpv socket: %w", err)
}
