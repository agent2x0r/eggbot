// Package lua hosts in-process gopher-lua scripts against the shared bind engine.
package lua

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	glua "github.com/yuin/gopher-lua"

	"eggbot/internal/script"
)

type Host struct {
	Eng *script.Engine
	mu  sync.Mutex
	vms map[string]*glua.LState
}

func New(eng *script.Engine) *Host {
	return &Host{Eng: eng, vms: map[string]*glua.LState{}}
}

func (h *Host) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	vms := h.vms
	h.vms = make(map[string]*glua.LState)
	for path, L := range vms {
		h.Eng.ClearFile(path)
		L.Close()
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
	var loadErrs []error
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".lua") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		if err := h.LoadFile(path); err != nil {
			h.Eng.Log.Error("lua load", "file", path, "err", err)
			loadErrs = append(loadErrs, fmt.Errorf("%s: %w", path, err))
		}
	}
	return errors.Join(loadErrs...)
}

func (h *Host) LoadFile(path string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	staged := path + "\x00loading"
	h.Eng.ClearFile(staged)
	L := glua.NewState(glua.Options{SkipOpenLibs: true})
	for _, fn := range []func(*glua.LState) int{
		glua.OpenBase, glua.OpenTable, glua.OpenString, glua.OpenMath,
	} {
		fn(L)
	}
	for _, name := range []string{"dofile", "loadfile", "load", "loadstring"} {
		L.SetGlobal(name, glua.LNil)
	}
	trusted := h.Eng.IsTrusted(path)
	h.install(L, path, staged, trusted)
	if err := L.DoFile(path); err != nil {
		h.Eng.ClearFile(staged)
		L.Close()
		return err
	}
	h.Eng.ReplaceFile(staged, path)
	old := h.vms[path]
	h.vms[path] = L
	if old != nil {
		old.Close()
	}
	h.Eng.Log.Info("loaded lua script", "file", path, "trusted", trusted)
	return nil
}

func (h *Host) install(L *glua.LState, path, bindFile string, trusted bool) {
	api := h.Eng.API
	L.SetGlobal("bind", L.NewFunction(func(L *glua.LState) int {
		typ := L.CheckString(1)
		fl := L.OptString(2, "-")
		mask := L.OptString(3, "*")
		var fn *glua.LFunction
		var name string
		switch L.Get(4).Type() {
		case glua.LTFunction:
			fn = L.ToFunction(4)
			name = fn.String()
		case glua.LTString:
			name = L.ToString(4)
		default:
			L.ArgError(4, "function or name expected")
			return 0
		}
		b := &script.Bind{
			Type:    typ,
			Flags:   fl,
			Mask:    mask,
			Name:    name,
			Engine:  "lua",
			File:    bindFile,
			Trusted: trusted,
		}
		b.Call = func(ev script.Event) bool {
			h.mu.Lock()
			defer h.mu.Unlock()
			if h.vms[path] != L {
				return true
			}
			L.SetTop(0)
			var lf *glua.LFunction
			if fn != nil {
				lf = fn
			} else {
				g := L.GetGlobal(name)
				f, ok := g.(*glua.LFunction)
				if !ok {
					return true
				}
				lf = f
			}
			L.Push(lf)
			L.Push(glua.LString(ev.Nick))
			L.Push(glua.LString(ev.Host))
			L.Push(glua.LString(ev.Handle))
			L.Push(glua.LString(ev.Channel))
			L.Push(glua.LString(ev.Text))
			if err := L.PCall(5, 1, nil); err != nil {
				h.Eng.Log.Error("lua bind", "name", name, "err", err)
				return true
			}
			ret := L.Get(-1)
			L.Pop(1)
			if ret == glua.LString("continue") || ret == glua.LTrue {
				return true
			}
			if ret == glua.LFalse {
				return false
			}
			return true
		}
		h.Eng.AddBind(b)
		return 0
	}))

	L.SetGlobal("say", L.NewFunction(func(L *glua.LState) int {
		api.Say(L.CheckString(1), L.CheckString(2))
		return 0
	}))
	L.SetGlobal("act", L.NewFunction(func(L *glua.LState) int {
		api.Act(L.CheckString(1), L.CheckString(2))
		return 0
	}))
	L.SetGlobal("notice", L.NewFunction(func(L *glua.LState) int {
		api.Notice(L.CheckString(1), L.CheckString(2))
		return 0
	}))
	L.SetGlobal("boot", L.NewFunction(func(L *glua.LState) int {
		api.Kick(L.CheckString(1), L.CheckString(2), L.OptString(3, "boot"))
		return 0
	}))
	L.SetGlobal("putserv", L.NewFunction(func(L *glua.LState) int {
		line := L.CheckString(1)
		if !script.RawAllowed(trusted, line) {
			return 0
		}
		api.Putserv(line)
		return 0
	}))
	L.SetGlobal("puthelp", L.NewFunction(func(L *glua.LState) int {
		line := L.CheckString(1)
		if script.RawAllowed(trusted, line) {
			api.Puthelp(line)
		}
		return 0
	}))
	L.SetGlobal("putquick", L.NewFunction(func(L *glua.LState) int {
		line := L.CheckString(1)
		if script.RawAllowed(trusted, line) {
			api.Putquick(line)
		}
		return 0
	}))
	L.SetGlobal("putlog", L.NewFunction(func(L *glua.LState) int {
		api.Putlog(L.CheckString(1))
		return 0
	}))
	L.SetGlobal("matchattr", L.NewFunction(func(L *glua.LState) int {
		L.Push(glua.LBool(api.MatchAttr(L.CheckString(1), L.CheckString(2), L.OptString(3, ""))))
		return 1
	}))
	L.SetGlobal("chattr", L.NewFunction(func(L *glua.LState) int {
		if !trusted {
			L.RaiseError("chattr requires a trusted script")
			return 0
		}
		err := api.Chattr(L.CheckString(1), L.CheckString(2), L.OptString(3, ""))
		if err != nil {
			L.Push(glua.LString(err.Error()))
			return 1
		}
		return 0
	}))
	L.SetGlobal("finduser", L.NewFunction(func(L *glua.LState) int {
		L.Push(glua.LString(api.Finduser(L.CheckString(1))))
		return 1
	}))
	L.SetGlobal("validuser", L.NewFunction(func(L *glua.LState) int {
		L.Push(glua.LBool(api.Validuser(L.CheckString(1))))
		return 1
	}))
	L.SetGlobal("onchan", L.NewFunction(func(L *glua.LState) int {
		L.Push(glua.LBool(api.Onchan(L.CheckString(1), L.CheckString(2))))
		return 1
	}))
	L.SetGlobal("isop", L.NewFunction(func(L *glua.LState) int {
		L.Push(glua.LBool(api.Isop(L.CheckString(1), L.CheckString(2))))
		return 1
	}))
	L.SetGlobal("isvoice", L.NewFunction(func(L *glua.LState) int {
		L.Push(glua.LBool(api.Isvoice(L.CheckString(1), L.CheckString(2))))
		return 1
	}))
	L.SetGlobal("botonchan", L.NewFunction(func(L *glua.LState) int {
		L.Push(glua.LBool(api.Botonchan(L.CheckString(1))))
		return 1
	}))
	L.SetGlobal("botisop", L.NewFunction(func(L *glua.LState) int {
		L.Push(glua.LBool(api.Botisop(L.CheckString(1))))
		return 1
	}))
	L.SetGlobal("topic", L.NewFunction(func(L *glua.LState) int {
		L.Push(glua.LString(api.Topic(L.CheckString(1))))
		return 1
	}))
	L.SetGlobal("chanlist", L.NewFunction(func(L *glua.LState) int {
		nicks := api.Chanlist(L.CheckString(1))
		tbl := L.NewTable()
		for i, n := range nicks {
			tbl.RawSetInt(i+1, glua.LString(n))
		}
		L.Push(tbl)
		return 1
	}))
	L.SetGlobal("botnick", L.NewFunction(func(L *glua.LState) int {
		L.Push(glua.LString(api.BotNick()))
		return 1
	}))
	L.SetGlobal("utimer", L.NewFunction(func(L *glua.LState) int {
		sec := L.CheckNumber(1)
		var call func()
		switch L.Get(2).Type() {
		case glua.LTFunction:
			fn := L.ToFunction(2)
			call = func() {
				h.mu.Lock()
				defer h.mu.Unlock()
				if h.vms[path] != L {
					return
				}
				L.SetTop(0)
				L.Push(fn)
				if err := L.PCall(0, 0, nil); err != nil {
					h.Eng.Log.Error("lua timer", "file", path, "err", err)
				}
			}
		case glua.LTString:
			name := L.ToString(2)
			call = func() {
				h.mu.Lock()
				defer h.mu.Unlock()
				if h.vms[path] != L {
					return
				}
				L.SetTop(0)
				g := L.GetGlobal(name)
				if f, ok := g.(*glua.LFunction); ok {
					L.Push(f)
					if err := L.PCall(0, 0, nil); err != nil {
						h.Eng.Log.Error("lua timer", "file", path, "err", err)
					}
				}
			}
		}
		id := h.Eng.AddTimerFor(bindFile, time.Duration(float64(sec)*float64(time.Second)), false, "utimer", call)
		L.Push(glua.LNumber(id))
		return 1
	}))
	L.SetGlobal("timer", L.NewFunction(func(L *glua.LState) int {
		min := L.CheckNumber(1)
		var call func()
		if L.Get(2).Type() == glua.LTFunction {
			fn := L.ToFunction(2)
			call = func() {
				h.mu.Lock()
				defer h.mu.Unlock()
				if h.vms[path] != L {
					return
				}
				L.SetTop(0)
				L.Push(fn)
				if err := L.PCall(0, 0, nil); err != nil {
					h.Eng.Log.Error("lua timer", "file", path, "err", err)
				}
			}
		}
		id := h.Eng.AddTimerFor(bindFile, time.Duration(float64(min)*float64(time.Minute)), true, "timer", call)
		L.Push(glua.LNumber(id))
		return 1
	}))
	L.SetGlobal("killtimer", L.NewFunction(func(L *glua.LState) int {
		h.Eng.KillTimer(L.CheckInt(1))
		return 0
	}))
}

func (h *Host) String() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return fmt.Sprintf("lua(%d vms)", len(h.vms))
}
