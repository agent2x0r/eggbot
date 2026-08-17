package lua

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"eggbot/internal/script"
)

func TestLoadDirAndAPI(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "api.lua")
	if err := os.WriteFile(path, []byte(`
bind("pubm", "-", "*", function(nick, host, handle, chan, text)
  say(chan, "x")
  act(chan, "y")
  notice(nick, "z")
  boot(chan, nick, "bye")
  putserv("PRIVMSG #c :ok")
  puthelp("PRIVMSG #c :h")
  putquick("MODE #c +v n")
  putlog("log")
  matchattr("nate", "n", "")
  chattr("bob", "+f", "")
  finduser("a!b@c")
  validuser("nate")
  onchan("n", "#c")
  isop("n", "#c")
  isvoice("n", "#c")
  botonchan("#c")
  botisop("#c")
  topic("#c")
  chanlist("#c")
  botnick()
end)
function named_timer()
end
id = utimer(0.2, "named_timer")
killtimer(id)
id2 = utimer(0.2, function() end)
killtimer(id2)
timer(60, function() end)
`), 0o600); err != nil {
		t.Fatal(err)
	}
	api := &fakeAPI{}
	eng := script.New(api, slog.Default(), []string{path})
	h := New(eng)
	defer h.Close()
	if err := h.LoadDir(""); err != nil {
		t.Fatal(err)
	}
	if err := h.LoadDir(filepath.Join(dir, "missing")); err != nil {
		t.Fatal(err)
	}
	if err := h.LoadDir(dir); err != nil {
		t.Fatal(err)
	}
	eng.Dispatch("pubm", script.Event{Nick: "n", Channel: "#c", Text: "hello"})
	if h.String() == "" {
		t.Fatal("string")
	}
}
