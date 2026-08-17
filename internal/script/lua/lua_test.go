package lua

import (
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"eggbot/internal/script"
	"eggbot/internal/userfile"
)

type fakeAPI struct {
	mu   sync.Mutex
	said []string
}

func (f *fakeAPI) Say(target, text string) {
	f.mu.Lock()
	f.said = append(f.said, target+":"+text)
	f.mu.Unlock()
}
func (f *fakeAPI) Act(target, text string)                      {}
func (f *fakeAPI) Notice(target, text string)                   {}
func (f *fakeAPI) Kick(channel, nick, reason string)            {}
func (f *fakeAPI) Putserv(line string)                          {}
func (f *fakeAPI) Puthelp(line string)                          {}
func (f *fakeAPI) Putquick(line string)                         {}
func (f *fakeAPI) Putlog(line string)                           {}
func (f *fakeAPI) MatchAttr(handle, flags, channel string) bool { return true }
func (f *fakeAPI) Chattr(handle, spec, channel string) error    { return nil }
func (f *fakeAPI) Finduser(nuh string) string                   { return "" }
func (f *fakeAPI) Validuser(handle string) bool                 { return false }
func (f *fakeAPI) Onchan(nick, channel string) bool             { return true }
func (f *fakeAPI) Isop(nick, channel string) bool               { return false }
func (f *fakeAPI) Isvoice(nick, channel string) bool            { return false }
func (f *fakeAPI) Botonchan(channel string) bool                { return true }
func (f *fakeAPI) Botisop(channel string) bool                  { return false }
func (f *fakeAPI) Topic(channel string) string                  { return "" }
func (f *fakeAPI) Chanlist(channel string) []string             { return nil }
func (f *fakeAPI) BotNick() string                              { return "eggbot" }
func (f *fakeAPI) Getuser(handle string) *userfile.User         { return nil }

func TestLuaPing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ping.lua")
	if err := os.WriteFile(path, []byte(`
bind("pub", "-", "!ping", "ping_pub")
function ping_pub(nick, host, handle, chan, text)
  say(chan, nick .. ": pong")
end
`), 0o644); err != nil {
		t.Fatal(err)
	}
	api := &fakeAPI{}
	eng := script.New(api, slog.Default(), nil)
	h := New(eng)
	if err := h.LoadFile(path); err != nil {
		t.Fatal(err)
	}
	eng.Dispatch("pub", script.Event{Nick: "nate", Host: "n!u@h", Channel: "#c", Text: "!ping"})
	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.said) != 1 || api.said[0] != "#c:nate: pong" {
		t.Fatalf("said %v", api.said)
	}
}

func TestFailedReloadKeepsPreviousLuaVM(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "reload.lua")
	if err := os.WriteFile(path, []byte(`
bind("pub", "-", "!ping", function(nick, host, handle, chan, text)
  say(chan, "old")
end)
`), 0o600); err != nil {
		t.Fatal(err)
	}
	api := &fakeAPI{}
	eng := script.New(api, slog.Default(), nil)
	h := New(eng)
	defer h.Close()
	if err := h.LoadFile(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`this is not valid lua (`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := h.LoadFile(path); err == nil {
		t.Fatal("expected reload failure")
	}
	eng.Dispatch("pub", script.Event{Channel: "#c", Text: "!ping"})
	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.said) != 1 || api.said[0] != "#c:old" {
		t.Fatalf("previous VM was not preserved: %v", api.said)
	}
}

func TestConcurrentLuaDispatchIsSerialized(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "concurrent.lua")
	if err := os.WriteFile(path, []byte(`
bind("pub", "-", "!ping", function(nick, host, handle, chan, text)
  say(chan, "pong")
end)
`), 0o600); err != nil {
		t.Fatal(err)
	}
	api := &fakeAPI{}
	eng := script.New(api, slog.Default(), nil)
	h := New(eng)
	defer h.Close()
	if err := h.LoadFile(path); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			eng.Dispatch("pub", script.Event{Channel: "#c", Text: "!ping"})
		}()
	}
	wg.Wait()
	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.said) != 20 {
		t.Fatalf("got %d callbacks, want 20", len(api.said))
	}
}
