//go:build windows

package mpv

import (
	"fmt"
	"net"
	"os"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/Microsoft/go-winio"
)

// Start runs mpv on url, drawn into wid, starting at from. The window's web
// view covers what mpv draws, so it is hidden until mpv ends.
func Start(bin, url string, from time.Duration, wid uintptr) (*Player, error) {
	pipe := fmt.Sprintf(`\\.\pipe\mygo-video-mpv-%d`, os.Getpid())
	hidden := findWebView(wid)
	if hidden != 0 {
		setVisible(hidden, false)
	}
	reveal := func() {
		if hidden != 0 {
			setVisible(hidden, true)
		}
	}
	cmd, err := launch(bin, url, from, wid, "--input-ipc-server="+pipe)
	if err != nil {
		reveal()
		return nil, err
	}
	p := &Player{cmd: cmd, reveal: reveal, replies: map[int]chan reply{}}
	conn, err := waitPipe(pipe, 5*time.Second)
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		reveal()
		return nil, err
	}
	p.conn = conn
	go p.readLoop()
	return p, nil
}

// waitPipe waits for mpv to open its socket: it takes a moment after start.
func waitPipe(pipe string, within time.Duration) (net.Conn, error) {
	deadline := time.Now().Add(within)
	var err error
	for time.Now().Before(deadline) {
		var conn net.Conn
		conn, err = winio.DialPipe(pipe, nil)
		if err == nil {
			return conn, nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return nil, fmt.Errorf("mpv socket: %w", err)
}

var (
	user32               = syscall.NewLazyDLL("user32.dll")
	procEnumChildWindows = user32.NewProc("EnumChildWindows")
	procGetClassName     = user32.NewProc("GetClassNameW")
	procShowWindow       = user32.NewProc("ShowWindow")
)

// findWebView is the window's web view, the child that covers what mpv draws.
func findWebView(parent uintptr) uintptr {
	var found uintptr
	cb := syscall.NewCallback(func(h, _ uintptr) uintptr {
		buf := make([]uint16, 256)
		procGetClassName.Call(h, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
		if strings.HasPrefix(syscall.UTF16ToString(buf), "Chrome_WidgetWin") {
			found = h
			return 0
		}
		return 1
	})
	procEnumChildWindows.Call(parent, cb, 0)
	return found
}

func setVisible(h uintptr, visible bool) {
	n := uintptr(0) // SW_HIDE
	if visible {
		n = 5 // SW_SHOW
	}
	procShowWindow.Call(h, n)
}
