package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/MoeForge/moejs"
	"github.com/MoeForge/moejs/eventloop"
	"github.com/MoeForge/moejs/webapi"
)

const (
	callTimeout = 30 * time.Second
	memoryLimit = 256 << 20
)

// Plugin is a lazily started JavaScript/TypeScript wallpaper source.
type Plugin struct {
	manifest Manifest
	dir      string
	dataDir  string
	module   *moejs.Module
	exports  map[string]bool

	mu       sync.Mutex
	settings map[string]string
	storage  map[string]json.RawMessage
	running  *runtimeState
	starting sync.Mutex
}

// Load validates and compiles a plugin without executing its code.
func Load(dir, dataDir string) (*Plugin, error) {
	manifest, err := readManifest(dir)
	if err != nil {
		return nil, err
	}
	module, err := compileEntry(dir, manifest)
	if err != nil {
		return nil, fmt.Errorf("编译插件入口失败: %w", err)
	}

	p := newPlugin(manifest, dir, dataDir, module)
	return p, p.readData()
}

// LoadSource loads a built-in plugin whose source is embedded in the binary.
// It follows the same manifest and runtime path as a user-installed plugin.
func LoadSource(manifest Manifest, filename, source, dataDir string) (*Plugin, error) {
	if err := validateManifest(&manifest); err != nil {
		return nil, err
	}
	if filename == "" {
		filename = manifest.Main
	}
	var module *moejs.Module
	var err error
	if filepath.Ext(filename) == ".ts" {
		module, err = moejs.CompileTS(filename, source)
	} else {
		module, err = moejs.Compile(filename, source)
	}
	if err != nil {
		return nil, fmt.Errorf("编译插件入口失败: %w", err)
	}
	p := newPlugin(manifest, "<embedded>/"+manifest.ID, dataDir, module)
	return p, p.readData()
}

func newPlugin(manifest Manifest, dir, dataDir string, module *moejs.Module) *Plugin {
	p := &Plugin{
		manifest: manifest,
		dir:      dir,
		dataDir:  filepath.Join(dataDir, manifest.ID),
		module:   module,
		exports:  make(map[string]bool),
		settings: make(map[string]string),
		storage:  make(map[string]json.RawMessage),
	}
	for _, name := range module.Exports() {
		p.exports[name] = true
	}
	return p
}

func (p *Plugin) readData() error {
	if err := p.readJSON("settings.json", &p.settings); err != nil {
		return err
	}
	if err := p.readJSON("storage.json", &p.storage); err != nil {
		return err
	}
	return nil
}

func (p *Plugin) readJSON(name string, target any) error {
	b, err := os.ReadFile(filepath.Join(p.dataDir, name))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, target); err != nil {
		return fmt.Errorf("解析插件数据 %s 失败: %w", name, err)
	}
	return nil
}

// ID returns the stable plugin ID.
func (p *Plugin) ID() string { return p.manifest.ID }

// Manifest returns a copy of the plugin manifest.
func (p *Plugin) Manifest() Manifest { return p.manifest }

// Has reports whether the plugin exports a capability.
func (p *Plugin) Has(name string) bool { return p.exports[name] }

// AllowsURL checks whether a plugin-produced image URL belongs to a declared
// network host. Downloads happen in the Go host, so they need the same
// boundary as host.fetch instead of trusting plugin-returned strings.
func (p *Plugin) AllowsURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	for _, allowed := range p.manifest.Permissions.Network {
		allowed = strings.ToLower(strings.TrimSpace(allowed))
		if allowed == host || (strings.HasPrefix(allowed, "*.") && strings.HasSuffix(host, allowed[1:])) {
			return true
		}
	}
	return false
}

// Setting returns a saved plugin setting.
func (p *Plugin) Setting(key string) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.settings[key]
}

// SetSetting saves a plugin setting and notifies the running plugin when it
// exports setSetting.
func (p *Plugin) SetSetting(key, value string) error {
	p.mu.Lock()
	p.settings[key] = value
	copySettings := make(map[string]string, len(p.settings))
	for k, v := range p.settings {
		copySettings[k] = v
	}
	running := p.running != nil
	p.mu.Unlock()
	if err := p.writeJSON("settings.json", copySettings); err != nil {
		return err
	}
	if running && p.Has("setSetting") {
		return p.Call(context.Background(), "setSetting", map[string]string{"key": key, "value": value}, nil)
	}
	return nil
}

// Close stops the plugin runtime. A later call starts a fresh runtime.
func (p *Plugin) Close() {
	p.starting.Lock()
	defer p.starting.Unlock()
	p.mu.Lock()
	running := p.running
	p.running = nil
	p.mu.Unlock()
	if running != nil {
		running.close()
	}
}

// Call invokes an exported plugin function and decodes its JSON result into
// out. A nil out discards the returned value.
func (p *Plugin) Call(ctx context.Context, method string, params any, out any) error {
	if !p.Has(method) {
		return ErrUnsupported
	}
	running, err := p.start()
	if err != nil {
		return err
	}

	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return err
	}
	answerCh := make(chan pluginAnswer, 1)
	if !running.loop.RunOnLoop(func(_ *moejs.Runtime) {
		running.ask(method, paramsJSON, func(answer pluginAnswer) { answerCh <- answer })
	}) {
		p.Close()
		return errors.New("插件运行时已停止")
	}

	callCtx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	select {
	case answer := <-answerCh:
		if answer.err != nil {
			return answer.err
		}
		if out == nil {
			return nil
		}
		return json.Unmarshal(answer.data, out)
	case <-callCtx.Done():
		p.Close()
		return fmt.Errorf("插件调用 %s 超时: %w", method, callCtx.Err())
	}
}

func (p *Plugin) start() (*runtimeState, error) {
	p.mu.Lock()
	if p.running != nil {
		running := p.running
		p.mu.Unlock()
		return running, nil
	}
	p.mu.Unlock()

	p.starting.Lock()
	defer p.starting.Unlock()
	p.mu.Lock()
	if p.running != nil {
		running := p.running
		p.mu.Unlock()
		return running, nil
	}
	p.mu.Unlock()

	if err := os.MkdirAll(p.dataDir, 0o700); err != nil {
		return nil, err
	}
	rt := moejs.NewRuntime(moejs.Options{MemoryLimit: memoryLimit})
	loop, err := eventloop.New(rt, eventloop.Options{
		OnError: func(err error) {
			log.Printf("wallpaper plugin %s: %v", p.ID(), err)
		},
	})
	if err != nil {
		return nil, err
	}
	api, err := webapi.Install(loop, webapi.Options{
		Client: &http.Client{Timeout: callTimeout},
		Allow:  webapi.AllowHosts(p.manifest.Permissions.Network...),
	})
	if err != nil {
		return nil, err
	}
	running := &runtimeState{plugin: p, rt: rt, loop: loop, api: api}
	if err := running.installHost(); err != nil {
		running.close()
		return nil, err
	}
	if err := rt.Load(p.module); err != nil && !errors.Is(err, moejs.ErrModulePending) {
		running.close()
		return nil, err
	}
	if err := loop.Start(); err != nil {
		running.close()
		return nil, err
	}
	p.mu.Lock()
	p.running = running
	p.mu.Unlock()
	return running, nil
}

func (p *Plugin) writeJSON(name string, value any) error {
	if err := os.MkdirAll(p.dataDir, 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	tmp := filepath.Join(p.dataDir, name+".part")
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(p.dataDir, name))
}

type pluginAnswer struct {
	data []byte
	err  error
}

type runtimeState struct {
	plugin *Plugin
	rt     *moejs.Runtime
	loop   *eventloop.Loop
	api    *webapi.API
	helper moejs.Value
	mu     sync.Mutex
	closed bool
}

var helperScript = func() *moejs.Script {
	script, err := moejs.CompileFunction("wallpaper-plugin-call", "fn, arg, ok, bad", `
const fail = (e) => {
  const isObject = e !== null && typeof e === "object";
  bad(String((isObject && e.code) || ""), isObject ? String(e.message || "") : String(e));
};
try {
  Promise.resolve(fn(arg)).then((result) => ok(result === undefined ? null : result), fail);
} catch (e) {
  fail(e);
}`, moejs.FunctionOptions{})
	if err != nil {
		panic(err)
	}
	return script
}()

func (r *runtimeState) installHost() error {
	if err := r.rt.SetGlobal("host", map[string]any{
		"setting": moejs.NativeFunc(func(_ *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
			return moejs.String(r.plugin.Setting(moejs.Arg(args, 0).String())), nil
		}),
		"log": moejs.NativeFunc(func(_ *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
			for _, arg := range args {
				log.Printf("wallpaper plugin %s: %s", r.plugin.ID(), arg.String())
			}
			return moejs.Undefined(), nil
		}),
		"storage": map[string]any{
			"get":    moejs.NativeFunc(r.storageGet),
			"set":    moejs.NativeFunc(r.storageSet),
			"remove": moejs.NativeFunc(r.storageRemove),
		},
	}); err != nil {
		return err
	}
	bridge, err := moejs.CompileScript("host-bridge.js", `host.fetch = (...args) => fetch(...args);`)
	if err != nil {
		return err
	}
	if _, err = r.rt.RunScript(bridge); err != nil {
		return err
	}
	r.helper, err = r.rt.RunScript(helperScript)
	return err
}

func (r *runtimeState) storageGet(_ *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
	key := moejs.Arg(args, 0).String()
	r.plugin.mu.Lock()
	data, ok := r.plugin.storage[key]
	r.plugin.mu.Unlock()
	if !ok {
		return moejs.Undefined(), nil
	}
	return r.rt.ParseJSON(data)
}

func (r *runtimeState) storageSet(_ *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
	key := moejs.Arg(args, 0).String()
	data, err := r.rt.AppendJSON(nil, moejs.Arg(args, 1))
	if err != nil {
		return moejs.Undefined(), err
	}
	r.plugin.mu.Lock()
	r.plugin.storage[key] = data
	copyStorage := make(map[string]json.RawMessage, len(r.plugin.storage))
	for k, v := range r.plugin.storage {
		copyStorage[k] = append(json.RawMessage(nil), v...)
	}
	r.plugin.mu.Unlock()
	return moejs.Undefined(), r.plugin.writeJSON("storage.json", copyStorage)
}

func (r *runtimeState) storageRemove(_ *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
	key := moejs.Arg(args, 0).String()
	r.plugin.mu.Lock()
	delete(r.plugin.storage, key)
	copyStorage := make(map[string]json.RawMessage, len(r.plugin.storage))
	for k, v := range r.plugin.storage {
		copyStorage[k] = append(json.RawMessage(nil), v...)
	}
	r.plugin.mu.Unlock()
	return moejs.Undefined(), r.plugin.writeJSON("storage.json", copyStorage)
}

func (r *runtimeState) ask(method string, params []byte, done func(pluginAnswer)) {
	fn, ok := r.rt.Export(method)
	if !ok {
		done(pluginAnswer{err: ErrUnsupported})
		return
	}
	arg, err := r.rt.ParseJSON(params)
	if err != nil {
		done(pluginAnswer{err: err})
		return
	}
	okFn := r.rt.Function("plugin-ok", 1, func(_ *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
		data, err := r.rt.AppendJSON(nil, moejs.Arg(args, 0))
		done(pluginAnswer{data: data, err: err})
		return moejs.Undefined(), nil
	})
	badFn := r.rt.Function("plugin-bad", 2, func(_ *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
		done(pluginAnswer{err: errors.New(moejs.Arg(args, 1).String())})
		return moejs.Undefined(), nil
	})
	_, err = r.rt.CallFunction(r.helper, moejs.Undefined(), fn, arg, okFn, badFn)
	if err != nil {
		done(pluginAnswer{err: err})
	}
}

func (r *runtimeState) close() {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	r.mu.Unlock()
	r.api.Close()
	r.loop.Terminate()
}
