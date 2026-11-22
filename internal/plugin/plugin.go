// Package plugin runs a source as JavaScript or TypeScript, in a runtime of
// its own inside the app. A plugin is a directory with a plugin.json and one
// module:
//
//	{
//	  "id": "demo", "name": "Demo", "version": "0.1.0", "apiVersion": 1,
//	  "main": "index.js", "playback": "stream",
//	  "permissions": {"network": ["api.example.com"]}
//	}
//
// and exports a function for each thing it does, by name: home, browse,
// search, title, play, open, progress, and the account's signIn, signOut and
// account. Each takes one object of parameters and answers a value or a
// promise of one. What it doesn't export, it doesn't do.
package plugin

import (
	"crypto/aes"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/MoeForge/moejs"
	"github.com/MoeForge/moejs/eventloop"
	"github.com/MoeForge/moejs/webapi"

	"mygo-video/internal/media"
)

// APIVersion is the plugin API this app speaks.
const APIVersion = 1

const (
	callTimeout = 30 * time.Second
	// A runtime that doesn't take on a task in this long is stuck in the
	// plugin's code, rather than waiting on something.
	stuckAfter  = time.Second
	memoryLimit = 256 << 20
)

var (
	// ErrSignIn says the listener has to sign in for this.
	ErrSignIn = errors.New("sign in first")
	// ErrSetup says a setting the plugin needs is missing.
	ErrSetup = errors.New("the plugin needs setting up")
	// ErrUnavailable says the video can't be played.
	ErrUnavailable = errors.New("not available")
	// ErrUnsupported says the plugin doesn't do this.
	ErrUnsupported = errors.New("not supported by this plugin")
	// ErrDRM says the stream is protected, which the app doesn't play.
	ErrDRM = errors.New("protected by DRM")
)

// Permissions are what a plugin asks for beyond its own sandbox.
type Permissions struct {
	// Network lists the hosts fetch can reach, *.example.com for those below
	// it, and * for any.
	Network []string `json:"network,omitempty"`
	// StreamHosts lists the hosts of the addresses a plugin may hand the app
	// to play, *.example.com for those below it, and * for any. A source
	// like a collection site speaks from a new CDN every week, which no
	// list can name; the plugin's fetch stays inside Network regardless.
	StreamHosts []string `json:"streamHosts,omitempty"`
}

// Login is the page a plugin signs in on, and the hosts whose cookies the
// app keeps for it.
type Login struct {
	URL         string   `json:"url"`
	CookieHosts []string `json:"cookieHosts,omitempty"`
}

// Manifest is a plugin's plugin.json.
type Manifest struct {
	media.Info
	APIVersion    int         `json:"apiVersion"`
	Main          string      `json:"main,omitempty"`
	MinAppVersion string      `json:"minAppVersion,omitempty"`
	Description   string      `json:"description,omitempty"`
	Permissions   Permissions `json:"permissions"`
	Login         *Login      `json:"login,omitempty"`
}

// appVersion is the app's version, which the app sets.
var appVersion string

// SetAppVersion says which version of the app this is.
func SetAppVersion(v string) { appVersion = v }

// openURL opens a page in the listener's browser. The app sets it.
var openURL = func(string) error { return errors.New("can't open pages here") }

// SetOpenURL says how to open a page in the listener's browser.
func SetOpenURL(open func(string) error) { openURL = open }

// Plugin is a media source in JavaScript. It starts on first use, and again
// after it got stuck.
type Plugin struct {
	manifest Manifest
	dir      string
	data     string
	mod      *moejs.Module
	exports  map[string]bool

	starting sync.Mutex

	mu       sync.Mutex
	settings map[string]string
	storage  map[string]json.RawMessage
	cookie   string
	ffmpeg   bool
	vm       *vm
	ws       map[string]*wsConn
	wsSeq    int32

	onDanmaku func(Cue)
}

// Load reads and compiles the plugin in dir, keeping its data in dataDir,
// without running it.
func Load(dir, dataDir string) (*Plugin, error) {
	m, err := readManifest(dir)
	if err != nil {
		return nil, err
	}
	js, err := os.ReadFile(filepath.Join(dir, m.Main))
	if err != nil {
		return nil, err
	}
	compile := moejs.Compile
	if strings.HasSuffix(m.Main, ".ts") {
		compile = moejs.CompileTS
	}
	mod, err := compile(m.Main, string(js))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", dir, err)
	}
	p := &Plugin{
		manifest: m,
		dir:      dir,
		data:     filepath.Join(dataDir, m.ID),
		mod:      mod,
		exports:  map[string]bool{},
		settings: map[string]string{},
		storage:  map[string]json.RawMessage{},
	}
	for _, name := range mod.Exports() {
		p.exports[name] = true
	}
	readJSON(filepath.Join(p.data, "settings.json"), &p.settings)
	readJSON(filepath.Join(p.data, "storage.json"), &p.storage)
	return p, nil
}

// readManifest reads plugin.json and checks what doesn't depend on the code.
func readManifest(dir string) (Manifest, error) {
	b, err := os.ReadFile(filepath.Join(dir, "plugin.json"))
	if err != nil {
		return Manifest{}, err
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return Manifest{}, fmt.Errorf("%s: %w", dir, err)
	}
	if m.ID == "" {
		return Manifest{}, fmt.Errorf("%s: plugin.json needs an id", dir)
	}
	if len(m.ID) > 64 || !pluginName.MatchString(m.ID) {
		return Manifest{}, fmt.Errorf("%s: the id may only use letters, digits, ., _ and -, and starts with a letter or digit: %q", dir, m.ID)
	}
	if m.APIVersion == 0 {
		m.APIVersion = APIVersion
	}
	if m.APIVersion > APIVersion {
		return Manifest{}, fmt.Errorf("%s: needs a newer app (apiVersion %d)", dir, m.APIVersion)
	}
	if m.Name == "" {
		m.Name = m.ID
	}
	if m.Playback == "" {
		m.Playback = "stream"
	}
	if m.Main == "" {
		m.Main = "index.js"
	}
	if !filepath.IsLocal(m.Main) {
		return Manifest{}, fmt.Errorf("%s: main must be a file in the plugin's folder: %q", dir, m.Main)
	}
	if m.Login != nil && m.Login.URL != "" {
		u, err := url.Parse(m.Login.URL)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
			return Manifest{}, fmt.Errorf("%s: login.url must be an http or https address", dir)
		}
	}
	if m.MinAppVersion != "" && appVersion != "" {
		if min, ok := parseVersion(m.MinAppVersion); ok {
			if now, ok := parseVersion(appVersion); ok && versionLess(now, min) {
				return Manifest{}, fmt.Errorf("%s: needs app %s or newer", dir, m.MinAppVersion)
			}
		}
	}
	return m, nil
}

// Entry is a plugin found on disk, whether or not it is on.
type Entry struct {
	Dir      string
	Manifest Manifest
	Err      error
}

// Scan lists the plugins in dir, reading only plugin.json: the code waits
// for the plugin to be enabled.
func Scan(dir string) []Entry {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []Entry
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		d := filepath.Join(dir, e.Name())
		m, err := readManifest(d)
		out = append(out, Entry{Dir: d, Manifest: m, Err: err})
	}
	return out
}

// Discover loads every plugin in dir. One that doesn't load is logged and
// left out.
func Discover(dir, dataDir string) []*Plugin {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var plugins []*Plugin
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		p, err := Load(filepath.Join(dir, e.Name()), dataDir)
		if err != nil {
			log.Printf("plugin: %v", err)
			continue
		}
		plugins = append(plugins, p)
	}
	return plugins
}

func readJSON(path string, into any) {
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, into)
	}
}

func (p *Plugin) writeJSON(name string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(p.data, 0o700); err != nil {
		return err
	}
	path := filepath.Join(p.data, name)
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return err
	}
	return nil
}

// vm is a running plugin: only its loop's goroutine touches the runtime.
type vm struct {
	rt     *moejs.Runtime
	loop   *eventloop.Loop
	api    *webapi.API
	helper moejs.Value

	inFlight  int
	closeOnce sync.Once
	done      chan struct{}
}

func (v *vm) close() {
	v.closeOnce.Do(func() {
		close(v.done)
		v.api.Close()
		v.loop.Terminate()
	})
}

const helperBody = `
const fail = (e) => {
  const isObject = e !== null && typeof e === "object";
  bad(String((isObject && e.code) || ""), isObject ? String(e.message || "") : String(e));
};
try {
  Promise.resolve(fn(arg)).then((r) => ok(r === undefined ? null : r), fail);
} catch (e) {
  fail(e);
}`

var helperScript = func() *moejs.Script {
	s, err := moejs.CompileFunction("host-call", "fn, arg, ok, bad", helperBody, moejs.FunctionOptions{})
	if err != nil {
		panic(err)
	}
	return s
}()

func (p *Plugin) running() (*vm, error) {
	if v := p.current(); v != nil {
		return v, nil
	}
	p.starting.Lock()
	defer p.starting.Unlock()
	if v := p.current(); v != nil {
		return v, nil
	}
	v, err := p.start()
	if err != nil {
		return nil, fmt.Errorf("starting plugin %s: %w", p.manifest.ID, err)
	}
	p.mu.Lock()
	p.vm = v
	p.mu.Unlock()
	return v, nil
}

func (p *Plugin) current() *vm {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.vm
}

func (p *Plugin) start() (*vm, error) {
	if err := os.MkdirAll(p.data, 0o700); err != nil {
		return nil, err
	}
	rt := moejs.NewRuntime(moejs.Options{MemoryLimit: memoryLimit})
	loop, err := eventloop.New(rt, eventloop.Options{
		OnError: func(err error) { log.Printf("plugin %s: %v", p.manifest.ID, err) },
	})
	if err != nil {
		return nil, err
	}
	api, err := webapi.Install(loop, p.webOptions())
	if err != nil {
		return nil, err
	}
	v := &vm{rt: rt, loop: loop, api: api, done: make(chan struct{})}
	if err := p.installHost(v); err != nil {
		api.Close()
		return nil, err
	}
	if v.helper, err = rt.RunScript(helperScript); err != nil {
		api.Close()
		return nil, err
	}
	if err := rt.Load(p.mod); err != nil && !errors.Is(err, moejs.ErrModulePending) {
		api.Close()
		return nil, err
	}
	if err := loop.Start(); err != nil {
		api.Close()
		return nil, err
	}
	return v, nil
}

// webOptions are what fetch may do.
func (p *Plugin) webOptions() webapi.Options {
	hosts := p.manifest.Permissions.Network
	if len(hosts) == 0 {
		return webapi.Options{}
	}
	opts := webapi.Options{Client: &http.Client{Timeout: 10 * time.Minute}}
	for _, h := range hosts {
		if h == "*" {
			return opts
		}
	}
	opts.Allow = webapi.AllowHosts(hosts...)
	return opts
}

type native = func(*moejs.Realm, moejs.Value, []moejs.Value) (moejs.Value, error)

// installHost gives the runtime the host global and console.
func (p *Plugin) installHost(v *vm) error {
	rt := v.rt
	logFn := func(_ *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
		parts := make([]string, len(args))
		for i, a := range args {
			parts[i] = a.String()
		}
		log.Printf("plugin %s: %s", p.manifest.ID, strings.Join(parts, " "))
		return moejs.Undefined(), nil
	}
	if err := rt.SetGlobal("console", map[string]any{
		"log":   moejs.NativeFunc(logFn),
		"info":  moejs.NativeFunc(logFn),
		"warn":  moejs.NativeFunc(logFn),
		"error": moejs.NativeFunc(logFn),
	}); err != nil {
		return err
	}
	return rt.SetGlobal("host", map[string]any{
		"platform": runtime.GOOS,
		"arch":     runtime.GOARCH,
		"dir":      p.dir,
		"dataDir":  p.data,
		"ffmpeg":   p.FFmpeg(),
		"storage": map[string]any{
			"get":    moejs.NativeFunc(p.storageGet(v)),
			"set":    moejs.NativeFunc(p.storageSet(v)),
			"remove": moejs.NativeFunc(p.storageRemove),
		},
		"language": moejs.NativeFunc(func(*moejs.Realm, moejs.Value, []moejs.Value) (moejs.Value, error) {
			return moejs.String("zh-CN"), nil
		}),
		"t": moejs.NativeFunc(func(_ *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
			return moejs.String(moejs.Arg(args, 0).String()), nil
		}),
		"setting": moejs.NativeFunc(func(_ *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
			return moejs.String(p.Setting(moejs.Arg(args, 0).String())), nil
		}),
		"cookie": moejs.NativeFunc(func(_ *moejs.Realm, _ moejs.Value, _ []moejs.Value) (moejs.Value, error) {
			return moejs.String(p.Cookie()), nil
		}),
		"openURL": moejs.NativeFunc(func(_ *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
			address := moejs.Arg(args, 0).String()
			u, err := url.Parse(address)
			if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
				return moejs.Undefined(), fmt.Errorf("not a web address: %s", address)
			}
			if !p.AllowsHost(address) && !p.AllowsStreamHost(address) {
				return moejs.Undefined(), fmt.Errorf("the plugin may not open %s", u.Hostname())
			}
			return moejs.Undefined(), openURL(address)
		}),
		"ws": map[string]any{
			"open": moejs.NativeFunc(func(_ *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
				url := moejs.Arg(args, 0).String()
				var headers map[string]string
				if len(args) > 1 {
					headers = wsHeaders(v, args[1])
				}
				id, err := p.wsOpen(v, url, headers)
				if err != nil {
					return moejs.Undefined(), err
				}
				return moejs.String(id), nil
			}),
			"on": moejs.NativeFunc(func(_ *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
				p.wsOn(moejs.Arg(args, 0).String(), moejs.Arg(args, 1).String(), moejs.Arg(args, 2))
				return moejs.Undefined(), nil
			}),
			"send": moejs.NativeFunc(func(_ *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
				data, _ := v.rt.ToGo(moejs.Arg(args, 1))
				b, _ := data.([]byte)
				if b == nil {
					b = []byte(moejs.Arg(args, 1).String())
				}
				return moejs.Undefined(), p.wsSend(moejs.Arg(args, 0).String(), b)
			}),
			"close": moejs.NativeFunc(func(_ *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
				p.wsClose(moejs.Arg(args, 0).String())
				return moejs.Undefined(), nil
			}),
		},
		"inflate": moejs.NativeFunc(func(realm *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
			b, _ := realm.ToGo(moejs.Arg(args, 0)).([]byte)
			out, err := inflateZlib(b)
			if err != nil {
				return moejs.Undefined(), err
			}
			return realm.FromGo(out)
		}),
		"text": moejs.NativeFunc(func(realm *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
			b, _ := realm.ToGo(moejs.Arg(args, 0)).([]byte)
			return moejs.String(string(b)), nil
		}),
		"emitDanmaku": moejs.NativeFunc(func(realm *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
			m, _ := realm.ToGo(moejs.Arg(args, 0)).(map[string]any)
			text, _ := m["text"].(string)
			if text == "" {
				return moejs.Undefined(), nil
			}
			color, _ := m["color"].(string)
			p.pushDanmaku(Cue{Text: text, Color: color, Time: time.Now().UnixMilli()})
			return moejs.Undefined(), nil
		}),
		"crypto": map[string]any{
			"md5": moejs.NativeFunc(func(_ *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
				sum := md5.Sum([]byte(moejs.Arg(args, 0).String()))
				return moejs.String(hex.EncodeToString(sum[:])), nil
			}),
			"sha256": moejs.NativeFunc(func(_ *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
				sum := sha256.Sum256([]byte(moejs.Arg(args, 0).String()))
				return moejs.String(hex.EncodeToString(sum[:])), nil
			}),
			"aesEcbEncrypt": moejs.NativeFunc(func(_ *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
				out, err := aesECB([]byte(moejs.Arg(args, 0).String()), []byte(moejs.Arg(args, 1).String()))
				if err != nil {
					return moejs.Undefined(), err
				}
				return moejs.String(hex.EncodeToString(out)), nil
			}),
		},
		"removeDataDir": moejs.NativeFunc(func(_ *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
			name := moejs.Arg(args, 0).String()
			if !filepath.IsLocal(name) || filepath.Clean(name) == "." {
				return moejs.Undefined(), fmt.Errorf("not a folder of the plugin's data: %s", name)
			}
			return moejs.Undefined(), os.RemoveAll(filepath.Join(p.data, name))
		}),
	})
}

func (p *Plugin) storageGet(v *vm) native {
	return func(_ *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
		p.mu.Lock()
		raw, ok := p.storage[moejs.Arg(args, 0).String()]
		p.mu.Unlock()
		if !ok {
			return moejs.Undefined(), nil
		}
		return v.rt.ParseJSON(raw)
	}
}

func (p *Plugin) storageSet(v *vm) native {
	return func(_ *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
		key := moejs.Arg(args, 0).String()
		if key == "" || len(key) > 256 {
			return moejs.Undefined(), fmt.Errorf("a storage key must be 1 to 256 bytes")
		}
		raw, err := v.rt.AppendJSON(nil, moejs.Arg(args, 1))
		if err != nil {
			return moejs.Undefined(), err
		}
		if len(raw) > storageLimit {
			return moejs.Undefined(), fmt.Errorf("a storage value may be at most %d bytes", storageLimit)
		}
		p.mu.Lock()
		if _, exists := p.storage[key]; !exists && len(p.storage) >= storageKeysLimit {
			p.mu.Unlock()
			return moejs.Undefined(), fmt.Errorf("the plugin has too many storage keys")
		}
		total := 0
		for k, old := range p.storage {
			if k != key {
				total += len(old)
			}
		}
		if total+len(raw) > storageLimit {
			p.mu.Unlock()
			return moejs.Undefined(), fmt.Errorf("the plugin's storage is full")
		}
		p.storage[key] = raw
		err = p.writeJSON("storage.json", p.storage)
		p.mu.Unlock()
		return moejs.Undefined(), err
	}
}

func (p *Plugin) storageRemove(_ *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.storage, moejs.Arg(args, 0).String())
	return moejs.Undefined(), p.writeJSON("storage.json", p.storage)
}

type answer struct {
	json []byte
	err  error
}

func asError(code, message string) error {
	var known error
	switch code {
	case "signIn":
		known = ErrSignIn
	case "setup":
		known = ErrSetup
	case "unavailable":
		known = ErrUnavailable
	case "unsupported":
		known = ErrUnsupported
	case "drm":
		known = ErrDRM
	default:
		if message == "" {
			return errors.New("the plugin failed")
		}
		return errors.New(message)
	}
	if message == "" {
		return known
	}
	return fmt.Errorf("%w: %s", known, message)
}

func (v *vm) ask(method string, params []byte, got func(answer)) {
	rt := v.rt
	fn, ok := rt.Export(method)
	if !ok {
		got(answer{err: ErrUnsupported})
		return
	}
	arg := moejs.Undefined()
	if params != nil {
		var err error
		if arg, err = rt.ParseJSON(params); err != nil {
			got(answer{err: err})
			return
		}
	}
	done := func(a answer) {
		got(a)
		v.inFlight--
		v.loop.RunOnLoop(func(rt *moejs.Runtime) {
			if v.inFlight == 0 {
				rt.ReleaseCallData()
			}
		})
	}
	v.inFlight++
	okFn := rt.Function("ok", 1, func(_ *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
		out, err := rt.AppendJSON(nil, moejs.Arg(args, 0))
		done(answer{json: out, err: err})
		return moejs.Undefined(), nil
	})
	badFn := rt.Function("bad", 2, func(_ *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
		done(answer{err: asError(moejs.Arg(args, 0).String(), moejs.Arg(args, 1).String())})
		return moejs.Undefined(), nil
	})
	if _, err := rt.CallFunction(v.helper, moejs.Undefined(), fn, arg, okFn, badFn); err != nil {
		done(answer{err: err})
	}
}

func (p *Plugin) call(method string, params, out any) error {
	if !p.exports[method] {
		return ErrUnsupported
	}
	v, err := p.running()
	if err != nil {
		return err
	}
	var encoded []byte
	if params != nil {
		if encoded, err = json.Marshal(params); err != nil {
			return err
		}
	}
	ch := make(chan answer, 1)
	if !v.loop.RunOnLoop(func(*moejs.Runtime) {
		v.ask(method, encoded, func(a answer) { ch <- a })
	}) {
		p.drop(v)
		return fmt.Errorf("the plugin stopped; try again")
	}
	timer := time.NewTimer(callTimeout)
	defer timer.Stop()
	select {
	case a := <-ch:
		if a.err != nil {
			return a.err
		}
		if out == nil {
			return nil
		}
		return json.Unmarshal(a.json, out)
	case <-v.done:
		return fmt.Errorf("the plugin stopped; try again")
	case <-timer.C:
		p.dropIfStuck(v)
		return fmt.Errorf("the plugin didn't answer %s in time", method)
	}
}

func (p *Plugin) dropIfStuck(v *vm) {
	alive := make(chan struct{})
	v.loop.RunOnLoop(func(*moejs.Runtime) { close(alive) })
	select {
	case <-alive:
	case <-time.After(stuckAfter):
		log.Printf("plugin %s: stuck; restarting it", p.manifest.ID)
		p.drop(v)
	}
}

func (p *Plugin) drop(v *vm) {
	p.mu.Lock()
	if p.vm == v {
		p.vm = nil
	}
	p.mu.Unlock()
	go v.close()
}

// Close stops the plugin.
func (p *Plugin) Close() {
	p.starting.Lock()
	defer p.starting.Unlock()
	p.closeWS()
	p.mu.Lock()
	v := p.vm
	p.vm = nil
	p.mu.Unlock()
	if v != nil {
		v.close()
	}
}

// SetDanmakuHandler takes the comments a live chat pushes.
func (p *Plugin) SetDanmakuHandler(fn func(Cue)) {
	p.mu.Lock()
	p.onDanmaku = fn
	p.mu.Unlock()
}

// pushDanmaku gives one comment to the app, if anyone listens.
func (p *Plugin) pushDanmaku(cue Cue) {
	p.mu.Lock()
	fn := p.onDanmaku
	p.mu.Unlock()
	if fn != nil {
		fn(cue)
	}
}

// Info is what plugin.json says of the plugin.
func (p *Plugin) Info() media.Info { return p.manifest.Info }

// Can reports whether the plugin exports the function.
func (p *Plugin) Can(what string) bool { return p.exports[what] }

// AllowsHost reports whether the plugin may reach a URL's host: the network
// permission names it, or *.
func (p *Plugin) AllowsHost(raw string) bool {
	return p.allowsHost(p.manifest.Permissions.Network, raw)
}

// AllowsStreamHost reports whether the plugin may hand the app an address to
// play: the streamHosts permission names its host, or *. A plugin that names
// no stream hosts may play the hosts it may already fetch.
func (p *Plugin) AllowsStreamHost(raw string) bool {
	if len(p.manifest.Permissions.StreamHosts) == 0 {
		return p.AllowsHost(raw)
	}
	return p.allowsHost(p.manifest.Permissions.StreamHosts, raw)
}

func (p *Plugin) allowsHost(list []string, raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := u.Hostname()
	for _, h := range list {
		if h == "*" || h == host {
			return true
		}
		if strings.HasPrefix(h, "*.") && strings.HasSuffix(host, h[1:]) {
			return true
		}
	}
	return false
}

// Setting is what the listener filled in.
func (p *Plugin) Setting(key string) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.settings[key]
}

// SetSetting keeps the setting.
func (p *Plugin) SetSetting(key, value string) error {
	p.mu.Lock()
	p.settings[key] = value
	settings := make(map[string]string, len(p.settings))
	for k, s := range p.settings {
		settings[k] = s
	}
	running := p.vm != nil
	p.mu.Unlock()
	if err := p.writeJSON("settings.json", settings); err != nil {
		return err
	}
	if running && p.exports["setSetting"] {
		return p.call("setSetting", map[string]string{"key": key, "value": value}, nil)
	}
	return nil
}

// aesECB encrypts with AES-ECB and PKCS#7 padding, as some services ask.
func aesECB(key, plain []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	pad := aes.BlockSize - len(plain)%aes.BlockSize
	buf := append(append([]byte(nil), plain...), bytesRepeat(byte(pad), pad)...)
	out := make([]byte, len(buf))
	for i := 0; i < len(buf); i += aes.BlockSize {
		block.Encrypt(out[i:], buf[i:])
	}
	return out, nil
}

func bytesRepeat(b byte, n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = b
	}
	return out
}

var pluginName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

const (
	storageLimit     = 4 << 20
	storageKeysLimit = 1000
)

// parseVersion reads the leading numbers of a dotted version, so "0.10.0 dev"
// and "v0.10.0" both mean 0.10.0.
func parseVersion(v string) ([]int, bool) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if v == "" {
		return nil, false
	}
	parts := strings.Split(v, ".")
	out := make([]int, 0, len(parts))
	for _, part := range parts {
		n, digits := 0, 0
		for _, r := range part {
			if r < '0' || r > '9' {
				break
			}
			n = n*10 + int(r-'0')
			digits++
		}
		if digits == 0 {
			break
		}
		out = append(out, n)
	}
	return out, len(out) > 0
}

func versionLess(a, b []int) bool {
	for i := 0; i < len(a) || i < len(b); i++ {
		x, y := 0, 0
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			return x < y
		}
	}
	return false
}

// CompareVersion compares two dotted versions: -1 when a is older, 0 when
// they match, and 1 when a is newer. A version it cannot read counts as a
// match.
func CompareVersion(a, b string) int {
	av, aok := parseVersion(a)
	bv, bok := parseVersion(b)
	if !aok || !bok {
		return 0
	}
	switch {
	case versionLess(av, bv):
		return -1
	case versionLess(bv, av):
		return 1
	}
	return 0
}
