package mpv

import (
	"bufio"
	"encoding/json"
	"net"
	"testing"
	"time"
)

// fakeMPV answers the calls the way mpv does, on the other end of a pipe.
func fakeMPV(t *testing.T, handle func(cmd []any) any) (net.Conn, *Player) {
	t.Helper()
	client, server := net.Pipe()
	t.Cleanup(func() { client.Close(); server.Close() })
	p := &Player{conn: client, replies: map[int]chan reply{}}
	go p.readLoop()
	go func() {
		sc := bufio.NewScanner(server)
		for sc.Scan() {
			var req struct {
				Command   []any `json:"command"`
				RequestID int   `json:"request_id"`
			}
			if json.Unmarshal(sc.Bytes(), &req) != nil {
				continue
			}
			out := map[string]any{"request_id": req.RequestID, "error": "success"}
			if handle != nil {
				out["data"] = handle(req.Command)
			}
			msg, _ := json.Marshal(out)
			_, _ = server.Write(append(msg, '\n'))
		}
	}()
	return client, p
}

func TestPosition(t *testing.T) {
	_, p := fakeMPV(t, func(cmd []any) any {
		if cmd[0] == "get_property" && cmd[1] == "time-pos" {
			return 90.25
		}
		return nil
	})
	if got, want := p.Position(), 90*time.Second+250*time.Millisecond; got != want {
		t.Fatalf("Position = %v, want %v", got, want)
	}
}

func TestCallError(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	p := &Player{conn: client, replies: map[int]chan reply{}}
	go p.readLoop()
	go func() {
		sc := bufio.NewScanner(server)
		sc.Scan()
		_, _ = server.Write([]byte(`{"request_id":0,"error":"property unavailable"}` + "\n"))
	}()
	if _, err := p.call(time.Second, "get_property", "time-pos"); err == nil {
		t.Fatal("call did not report the error mpv gave")
	}
}

func TestCallTimesOut(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	p := &Player{conn: client, replies: map[int]chan reply{}}
	go p.readLoop()
	// The reader takes the request and answers nothing, which is what a
	// stuck mpv looks like.
	go func() {
		sc := bufio.NewScanner(server)
		for sc.Scan() {
		}
	}()
	start := time.Now()
	if _, err := p.call(80*time.Millisecond, "get_property", "time-pos"); err == nil {
		t.Fatal("call did not time out")
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("timeout took %v", d)
	}
}

func TestSeekAndPause(t *testing.T) {
	var seen []string
	_, p := fakeMPV(t, func(cmd []any) any {
		seen = append(seen, cmd[0].(string))
		return nil
	})
	p.Seek(42 * time.Second)
	p.Pause(true)
	if len(seen) != 2 || seen[0] != "seek" || seen[1] != "set_property" {
		t.Fatalf("commands = %v", seen)
	}
}
