package script

import (
	"testing"
	"time"
)

func TestTrustedPatternsDoNotUseSubstringMatching(t *testing.T) {
	e := New(nil, nil, []string{"scripts/lua/admin.lua", "trusted.py"})
	if !e.IsTrusted("/srv/eggbot/scripts/lua/admin.lua") {
		t.Fatal("exact relative path should match")
	}
	if !e.IsTrusted("/srv/eggbot/scripts/python/trusted.py") {
		t.Fatal("exact basename should match")
	}
	if e.IsTrusted("/srv/eggbot/scripts/lua/not-admin.lua") {
		t.Fatal("substring path must not become trusted")
	}
	if e.IsTrusted("/srv/eggbot/scripts/python/untrusted.py") {
		t.Fatal("substring filename must not become trusted")
	}
}

func TestRawAllowed(t *testing.T) {
	for _, line := range []string{
		"PASS secret",
		"AUTHENTICATE payload",
		"QUIT :bye",
		"PRIVMSG NickServ :identify secret",
		"PRIVMSG #safe :hello\r\nMODE #safe +o attacker",
	} {
		if RawAllowed(false, line) {
			t.Fatalf("untrusted raw line allowed: %q", line)
		}
	}
	if !RawAllowed(false, "PRIVMSG #safe :hello") {
		t.Fatal("ordinary untrusted raw message should be allowed")
	}
	if RawAllowed(true, "PRIVMSG #safe :hello\nQUIT") {
		t.Fatal("protocol delimiters must be rejected even for trusted scripts")
	}
}

func TestReplaceFileReplacesBindsAndTimers(t *testing.T) {
	e := New(nil, nil, nil)
	e.AddBind(&Bind{File: "plugin.lua", Name: "old"})
	e.AddBind(&Bind{File: "plugin.lua\x00loading", Name: "new"})
	e.AddTimerFor("plugin.lua", time.Second, false, "old", nil)
	e.AddTimerFor("plugin.lua\x00loading", time.Second, false, "new", nil)

	e.ReplaceFile("plugin.lua\x00loading", "plugin.lua")

	if len(e.binds) != 1 || e.binds[0].Name != "new" || e.binds[0].File != "plugin.lua" {
		t.Fatalf("binds not replaced: %+v", e.binds)
	}
	if len(e.timers) != 1 || e.timers[0].Name != "new" || e.timers[0].File != "plugin.lua" {
		t.Fatalf("timers not replaced: %+v", e.timers)
	}
}
