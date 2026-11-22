package plugin

import (
	"bytes"
	"compress/zlib"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"

	"github.com/MoeForge/moejs"
	"github.com/coder/websocket"
)

// wsConn is one WebSocket a plugin opened, and what to call when it says
// something.
type wsConn struct {
	ctx       context.Context
	stop      context.CancelFunc
	c         *websocket.Conn
	mu        sync.Mutex
	events    map[string]moejs.Value
	pending   [][]byte
	closed    bool
	closeOnce sync.Once
}

// wsOpen opens a WebSocket and returns its id at once; open, message and
// close come later as events.
func (p *Plugin) wsOpen(v *vm, address string, headers map[string]string) (string, error) {
	u, err := url.Parse(address)
	if err != nil || (u.Scheme != "ws" && u.Scheme != "wss") {
		return "", fmt.Errorf("not a websocket address: %s", address)
	}
	if !p.AllowsHost(address) {
		return "", fmt.Errorf("the plugin may not reach %s", u.Hostname())
	}
	id := fmt.Sprintf("ws%d", atomic.AddInt32(&p.wsSeq, 1))
	ctx, stop := context.WithCancel(context.Background())
	conn := &wsConn{ctx: ctx, stop: stop, events: map[string]moejs.Value{}}
	p.mu.Lock()
	if p.ws == nil {
		p.ws = map[string]*wsConn{}
	}
	p.ws[id] = conn
	p.mu.Unlock()
	go func() {
		opts := &websocket.DialOptions{}
		if len(headers) > 0 {
			opts.HTTPHeader = http.Header{}
			for k, val := range headers {
				opts.HTTPHeader.Set(k, val)
			}
		}
		c, _, err := websocket.Dial(ctx, address, opts)
		if err != nil {
			conn.mu.Lock()
			conn.closed = true
			conn.mu.Unlock()
			p.wsFireClose(v, conn)
			p.wsDrop(id)
			return
		}
		conn.mu.Lock()
		if conn.closed {
			conn.mu.Unlock()
			c.Close(websocket.StatusNormalClosure, "")
			return
		}
		conn.c = c
		pending := conn.pending
		conn.pending = nil
		conn.mu.Unlock()
		for _, data := range pending {
			if err := c.Write(ctx, websocket.MessageBinary, data); err != nil {
				break
			}
		}
		p.wsFire(v, conn, "open", nil)
		for {
			_, data, err := c.Read(ctx)
			if err != nil {
				break
			}
			p.wsFire(v, conn, "message", data)
		}
		conn.mu.Lock()
		conn.closed = true
		conn.mu.Unlock()
		c.Close(websocket.StatusNormalClosure, "")
		p.wsFireClose(v, conn)
		p.wsDrop(id)
	}()
	return id, nil
}

func (p *Plugin) wsFireClose(v *vm, conn *wsConn) {
	conn.closeOnce.Do(func() { p.wsFire(v, conn, "close", nil) })
}

// wsFire calls the plugin's handler of an event on the loop that runs it.
func (p *Plugin) wsFire(v *vm, conn *wsConn, event string, data []byte) {
	conn.mu.Lock()
	fn, ok := conn.events[event]
	conn.mu.Unlock()
	if !ok {
		return
	}
	v.loop.RunOnLoop(func(rt *moejs.Runtime) {
		var arg moejs.Value = moejs.Undefined()
		if data != nil {
			if val, err := rt.FromGo(data); err == nil {
				arg = val
			}
		}
		_, _ = rt.CallFunction(fn, moejs.Undefined(), arg)
	})
}

func (p *Plugin) wsOn(id, event string, fn moejs.Value) {
	p.mu.Lock()
	conn := p.ws[id]
	p.mu.Unlock()
	if conn == nil {
		return
	}
	conn.mu.Lock()
	conn.events[event] = fn
	conn.mu.Unlock()
}

func (p *Plugin) wsSend(id string, data []byte) error {
	p.mu.Lock()
	conn := p.ws[id]
	p.mu.Unlock()
	if conn == nil {
		return fmt.Errorf("no websocket %s", id)
	}
	conn.mu.Lock()
	if conn.closed {
		conn.mu.Unlock()
		return fmt.Errorf("websocket %s is closed", id)
	}
	if conn.c == nil {
		if len(conn.pending) >= 64 {
			conn.mu.Unlock()
			return fmt.Errorf("websocket %s has too many messages waiting", id)
		}
		conn.pending = append(conn.pending, append([]byte(nil), data...))
		conn.mu.Unlock()
		return nil
	}
	c := conn.c
	conn.mu.Unlock()
	return c.Write(conn.ctx, websocket.MessageBinary, data)
}

func (p *Plugin) wsClose(id string) {
	p.mu.Lock()
	conn := p.ws[id]
	p.mu.Unlock()
	if conn == nil {
		return
	}
	conn.mu.Lock()
	if conn.closed {
		conn.mu.Unlock()
		return
	}
	conn.closed = true
	conn.stop()
	c := conn.c
	conn.mu.Unlock()
	if c != nil {
		c.Close(websocket.StatusNormalClosure, "")
	}
	p.wsDrop(id)
}

func (p *Plugin) wsDrop(id string) {
	p.mu.Lock()
	delete(p.ws, id)
	p.mu.Unlock()
}

// closeWS closes every WebSocket the plugin opened.
func (p *Plugin) closeWS() {
	p.mu.Lock()
	conns := make([]*wsConn, 0, len(p.ws))
	for id, c := range p.ws {
		conns = append(conns, c)
		delete(p.ws, id)
	}
	p.mu.Unlock()
	for _, c := range conns {
		c.stop()
		c.mu.Lock()
		c.closed = true
		if c.c != nil {
			c.c.Close(websocket.StatusGoingAway, "")
		}
		c.mu.Unlock()
	}
}

// inflate reads a zlib stream, as a live chat compresses its packets.
func inflateZlib(data []byte) ([]byte, error) {
	r, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}

// wsHeaders reads the headers a plugin asked for.
func wsHeaders(v *vm, val moejs.Value) map[string]string {
	out := map[string]string{}
	got, err := v.rt.ToGo(val)
	if err != nil {
		return out
	}
	m, ok := got.(map[string]any)
	if !ok {
		return out
	}
	for k, x := range m {
		if s, ok := x.(string); ok {
			out[k] = s
		}
	}
	return out
}
