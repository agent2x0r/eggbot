// Package python runs script plugins as a subprocess over JSON lines.
package python

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"eggbot/internal/script"
)

type Host struct {
	Eng     *script.Engine
	Bin     string
	SDKPath string
	mu      sync.Mutex
	procs   map[string]*proc
	all     map[*proc]struct{}
	closed  bool
}

type proc struct {
	cmd       *exec.Cmd
	enc       *json.Encoder
	stdin     io.WriteCloser
	file      string
	mu        sync.Mutex
	writeMu   sync.Mutex
	binds     map[int]*script.Bind
	pending   map[int]chan result
	req       int
	active    bool
	done      chan struct{}
	ready     chan struct{}
	activate  chan struct{}
	stopOnce  sync.Once
	readyOnce sync.Once
}

type result struct {
	OK    bool
	Value any
	Error string
}

func New(eng *script.Engine, bin, sdkDir string) *Host {
	if bin == "" {
		bin = "python3"
	}
	return &Host{
		Eng: eng, Bin: bin, SDKPath: sdkDir,
		procs: make(map[string]*proc),
		all:   make(map[*proc]struct{}),
	}
}

func (h *Host) Close() {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.closed = true
	procs := make([]*proc, 0, len(h.all))
	for p := range h.all {
		procs = append(procs, p)
	}
	h.procs = make(map[string]*proc)
	h.all = make(map[*proc]struct{})
	h.mu.Unlock()
	for _, p := range procs {
		h.Eng.ClearFile(p.file)
		p.stop()
	}
}

func (h *Host) LoadDir(dir string) error {
	if dir == "" {
		return nil
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var plugins []string
	for _, e := range ents {
		if e.IsDir() || e.Name() == "eggbot.py" || !strings.HasSuffix(e.Name(), ".py") {
			continue
		}
		if strings.HasPrefix(e.Name(), "_") {
			continue
		}
		plugins = append(plugins, filepath.Join(dir, e.Name()))
	}
	if len(plugins) == 0 {
		return nil
	}
	if _, err := exec.LookPath(h.Bin); err != nil {
		h.Eng.Log.Info("python scripts skipped (interpreter not found)", "bin", h.Bin)
		return nil
	}
	var loadErrs []error
	for _, path := range plugins {
		if err := h.LoadFile(path); err != nil {
			h.Eng.Log.Error("python load", "file", path, "err", err)
			loadErrs = append(loadErrs, fmt.Errorf("%s: %w", path, err))
		}
	}
	return errors.Join(loadErrs...)
}

func (h *Host) LoadFile(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	trusted := h.Eng.IsTrusted(abs)
	if !trusted {
		return fmt.Errorf("python plugin %s requires an explicit scripts.trusted entry (python has host OS access)", path)
	}
	sdk := h.SDKPath
	cmd := exec.Command(h.Bin, "-u", abs)
	configureProcess(cmd)
	cmd.Env = []string{
		"EGGBOT_PLUGIN=1",
		"HOME=" + os.TempDir(),
		"LANG=C",
		"PATH=/usr/bin:/bin",
	}
	if sdk != "" {
		cmd.Env = append(cmd.Env, "PYTHONPATH="+sdk)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start python %s: %w", path, err)
	}
	p := &proc{
		cmd:      cmd,
		enc:      json.NewEncoder(stdin),
		stdin:    stdin,
		file:     abs,
		binds:    map[int]*script.Bind{},
		pending:  map[int]chan result{},
		done:     make(chan struct{}),
		ready:    make(chan struct{}),
		activate: make(chan struct{}),
	}
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		p.stop()
		return fmt.Errorf("python host is closed")
	}
	h.all[p] = struct{}{}
	h.mu.Unlock()
	go p.readLoop(h, trusted, stdout)
	select {
	case <-p.ready:
	case <-p.done:
		return fmt.Errorf("python plugin %s exited before handshake", path)
	case <-time.After(3 * time.Second):
		p.stop()
		return fmt.Errorf("python plugin %s did not complete handshake", path)
	}

	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		p.stop()
		return fmt.Errorf("python host is closed")
	}
	old := h.procs[abs]
	h.procs[abs] = p
	h.mu.Unlock()
	h.Eng.ClearFile(abs)
	p.mu.Lock()
	p.active = true
	p.mu.Unlock()
	close(p.activate)
	if old != nil {
		old.stop()
	}
	h.Eng.Log.Info("loaded python script", "file", abs, "trusted", trusted)
	return nil
}

func (p *proc) readLoop(h *Host, trusted bool, stdout io.Reader) {
	defer func() {
		p.stop()
		_ = p.cmd.Wait()
		h.procExited(p)
	}()
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var msg map[string]any
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			h.Eng.Log.Debug("python bad json", "err", err)
			continue
		}
		op, _ := msg["op"].(string)
		switch op {
		case "hello":
			p.readyOnce.Do(func() { close(p.ready) })
			select {
			case <-p.activate:
			case <-p.done:
				return
			}
		case "bind":
			p.onBind(h, msg, trusted)
		case "call":
			p.onCall(h, msg, trusted)
		case "result":
			p.onResult(msg)
		}
	}
}

func (p *proc) stop() {
	p.stopOnce.Do(func() {
		p.mu.Lock()
		p.active = false
		p.mu.Unlock()
		close(p.done)
		_ = p.stdin.Close()
		killProcess(p.cmd)
	})
}

func (h *Host) procExited(p *proc) {
	h.mu.Lock()
	delete(h.all, p)
	active := h.procs[p.file] == p
	if active {
		delete(h.procs, p.file)
	}
	h.mu.Unlock()
	if active {
		h.Eng.ClearFile(p.file)
	}
}

func (p *proc) isActive() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.active
}

func (p *proc) send(msg map[string]any) error {
	done := make(chan error, 1)
	go func() {
		p.writeMu.Lock()
		err := p.enc.Encode(msg)
		p.writeMu.Unlock()
		done <- err
	}()
	select {
	case err := <-done:
		return err
	case <-p.done:
		return fmt.Errorf("python plugin stopped")
	case <-time.After(2 * time.Second):
		p.stop()
		return fmt.Errorf("python plugin write timed out")
	}
}

func (p *proc) onBind(h *Host, msg map[string]any, trusted bool) {
	if !p.isActive() {
		return
	}
	typ, _ := msg["type"].(string)
	flags, _ := msg["flags"].(string)
	mask, _ := msg["mask"].(string)
	name, _ := msg["name"].(string)
	id := intFrom(msg["id"])
	if flags == "" {
		flags = "-"
	}
	if mask == "" {
		mask = "*"
	}
	b := &script.Bind{
		Type: typ, Flags: flags, Mask: mask, Name: name,
		Engine: "python", File: p.file, Trusted: trusted,
	}
	b.Call = func(ev script.Event) bool {
		return p.fire(id, ev)
	}
	p.mu.Lock()
	p.binds[id] = b
	p.mu.Unlock()
	h.Eng.AddBind(b)
}

func (p *proc) fire(id int, ev script.Event) bool {
	p.mu.Lock()
	if !p.active {
		p.mu.Unlock()
		return true
	}
	p.req++
	req := p.req
	ch := make(chan result, 1)
	p.pending[req] = ch
	p.mu.Unlock()
	if err := p.send(map[string]any{
		"op": "event", "req": req, "id": id,
		"args": map[string]any{
			"nick": ev.Nick, "host": ev.Host, "handle": ev.Handle,
			"channel": ev.Channel, "text": ev.Text, "type": ev.Type,
		},
	}); err != nil {
		p.mu.Lock()
		delete(p.pending, req)
		p.mu.Unlock()
		return true
	}
	timer := time.NewTimer(8 * time.Second)
	defer timer.Stop()
	defer func() {
		p.mu.Lock()
		delete(p.pending, req)
		p.mu.Unlock()
	}()
	select {
	case r := <-ch:
		if v, ok := r.Value.(bool); ok {
			return v
		}
		return true
	case <-timer.C:
		return true
	case <-p.done:
		return true
	}
}

func (p *proc) onResult(msg map[string]any) {
	req := intFrom(msg["req"])
	p.mu.Lock()
	ch := p.pending[req]
	delete(p.pending, req)
	p.mu.Unlock()
	if ch == nil {
		return
	}
	ok, _ := msg["ok"].(bool)
	select {
	case ch <- result{OK: ok, Value: msg["value"], Error: str(msg["error"])}:
	default:
	}
}

func (p *proc) onCall(h *Host, msg map[string]any, trusted bool) {
	if !p.isActive() {
		return
	}
	req := intFrom(msg["req"])
	fn, _ := msg["fn"].(string)
	args, _ := msg["args"].([]any)
	api := h.Eng.API
	var err error
	var value any
	arg := func(i int) string {
		if i < len(args) {
			return fmt.Sprint(args[i])
		}
		return ""
	}
	switch fn {
	case "say":
		api.Say(arg(0), arg(1))
	case "act":
		api.Act(arg(0), arg(1))
	case "notice":
		api.Notice(arg(0), arg(1))
	case "kick", "boot":
		api.Kick(arg(0), arg(1), arg(2))
	case "putserv":
		if script.RawAllowed(trusted, arg(0)) {
			api.Putserv(arg(0))
		}
	case "puthelp":
		if script.RawAllowed(trusted, arg(0)) {
			api.Puthelp(arg(0))
		}
	case "putquick":
		if script.RawAllowed(trusted, arg(0)) {
			api.Putquick(arg(0))
		}
	case "putlog":
		api.Putlog(arg(0))
	case "matchattr":
		value = api.MatchAttr(arg(0), arg(1), arg(2))
	case "chattr":
		if !trusted {
			err = fmt.Errorf("chattr requires a trusted script")
		} else {
			err = api.Chattr(arg(0), arg(1), arg(2))
		}
	case "finduser":
		value = api.Finduser(arg(0))
	case "validuser":
		value = api.Validuser(arg(0))
	case "onchan":
		value = api.Onchan(arg(0), arg(1))
	case "isop":
		value = api.Isop(arg(0), arg(1))
	case "isvoice":
		value = api.Isvoice(arg(0), arg(1))
	case "botonchan":
		value = api.Botonchan(arg(0))
	case "botisop":
		value = api.Botisop(arg(0))
	case "topic":
		value = api.Topic(arg(0))
	case "chanlist":
		value = api.Chanlist(arg(0))
	case "botnick":
		value = api.BotNick()
	default:
		err = fmt.Errorf("unknown api %s", fn)
	}
	out := map[string]any{"op": "ret", "req": req, "ok": err == nil, "value": value}
	if err != nil {
		out["error"] = err.Error()
	}
	_ = p.send(out)
}

func intFrom(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	}
	return 0
}

func str(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}
