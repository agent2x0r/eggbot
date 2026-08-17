package python

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"eggbot/internal/script"
	"eggbot/internal/userfile"
)

type fakeAPI struct{}

func (fakeAPI) Say(string, string)                    {}
func (fakeAPI) Act(string, string)                    {}
func (fakeAPI) Notice(string, string)                 {}
func (fakeAPI) Kick(string, string, string)           {}
func (fakeAPI) Putserv(string)                        {}
func (fakeAPI) Puthelp(string)                        {}
func (fakeAPI) Putquick(string)                       {}
func (fakeAPI) Putlog(string)                         {}
func (fakeAPI) MatchAttr(string, string, string) bool { return true }
func (fakeAPI) Chattr(string, string, string) error   { return nil }
func (fakeAPI) Finduser(string) string                { return "nate" }
func (fakeAPI) Validuser(string) bool                 { return true }
func (fakeAPI) Onchan(string, string) bool            { return true }
func (fakeAPI) Isop(string, string) bool              { return false }
func (fakeAPI) Isvoice(string, string) bool           { return false }
func (fakeAPI) Botonchan(string) bool                 { return true }
func (fakeAPI) Botisop(string) bool                   { return false }
func (fakeAPI) Topic(string) string                   { return "t" }
func (fakeAPI) Chanlist(string) []string              { return []string{"a"} }
func (fakeAPI) BotNick() string                       { return "eggbot" }
func (fakeAPI) Getuser(string) *userfile.User         { return nil }

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

func TestLoadDirSkipsAndEmpty(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "_skip.py"), []byte(""), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "eggbot.py"), []byte(""), 0o600)
	eng := script.New(fakeAPI{}, slog.Default(), nil)
	h := New(eng, "/bin/sh", dir)
	if err := h.LoadDir(dir); err != nil {
		t.Fatal(err)
	}
	plugin := filepath.Join(dir, "plugin.py")
	if err := os.WriteFile(plugin, []byte("# fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := h.LoadDir(dir); err == nil {
		t.Fatal("untrusted plugin should fail")
	}
	if err := h.LoadDir(""); err != nil {
		t.Fatal(err)
	}
	if err := h.LoadDir(filepath.Join(dir, "missing")); err != nil {
		t.Fatal(err)
	}
}

func TestOnBindCallAndHelpers(t *testing.T) {
	eng := script.New(fakeAPI{}, slog.Default(), nil)
	h := New(eng, "python3", "")
	var buf bytes.Buffer
	p := &proc{
		enc:      json.NewEncoder(&buf),
		stdin:    nopWriteCloser{&buf},
		file:     "plugin.py",
		binds:    map[int]*script.Bind{},
		pending:  map[int]chan result{},
		done:     make(chan struct{}),
		ready:    make(chan struct{}),
		activate: make(chan struct{}),
		active:   true,
	}
	p.onBind(h, map[string]any{"type": "pubm", "id": 1.0, "mask": "*", "name": "all"}, true)
	if len(eng.Binds()) != 1 {
		t.Fatal("bind")
	}
	for _, fn := range []string{
		"say", "act", "notice", "kick", "putserv", "puthelp", "putquick", "putlog",
		"matchattr", "chattr", "finduser", "validuser", "onchan", "isop", "isvoice",
		"botonchan", "botisop", "topic", "chanlist", "botnick", "nope",
	} {
		p.onCall(h, map[string]any{"req": 1.0, "fn": fn, "args": []any{"#c", "x", "y"}}, true)
	}
	p.onCall(h, map[string]any{"req": 2.0, "fn": "chattr", "args": []any{"bob", "+f", ""}}, false)
	p.onResult(map[string]any{"req": 99.0, "ok": true})
	ch := make(chan result, 1)
	p.pending[7] = ch
	p.onResult(map[string]any{"req": 7.0, "ok": true, "value": true, "error": nil})
	select {
	case r := <-ch:
		if !r.OK {
			t.Fatal(r)
		}
	case <-time.After(time.Second):
		t.Fatal("result")
	}
	if intFrom(3) != 3 || intFrom(json.Number("4")) != 4 || intFrom("x") != 0 {
		t.Fatal("intFrom")
	}
	if str(nil) != "" || str("a") != "a" {
		t.Fatal("str")
	}
	inactive := &proc{done: make(chan struct{})}
	inactive.onBind(h, map[string]any{"id": 1.0}, true)
	inactive.onCall(h, map[string]any{"fn": "say"}, true)
	if inactive.fire(1, script.Event{}) != true {
		t.Fatal("inactive fire")
	}
	var buf2 bytes.Buffer
	p2 := &proc{
		enc:      json.NewEncoder(&buf2),
		stdin:    nopWriteCloser{&buf2},
		file:     "plugin.py",
		binds:    map[int]*script.Bind{},
		pending:  map[int]chan result{},
		done:     make(chan struct{}),
		ready:    make(chan struct{}),
		activate: make(chan struct{}),
		active:   true,
	}
	go func() {
		time.Sleep(20 * time.Millisecond)
		p2.onResult(map[string]any{"req": 1.0, "ok": true, "value": true})
	}()
	if !p2.fire(1, script.Event{Text: "hi"}) {
		t.Fatal("fire")
	}
}
