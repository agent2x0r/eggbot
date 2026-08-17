package config

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "c.toml")
	if err := os.WriteFile(p, []byte(`
nick = "eggy"
[server]
host = "irc.example.net"
port = 6667
tls = false
[owners]
handles = ["nate"]
[channel."#cool"]
chanset = "+ai +seen"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Nick != "eggy" || cfg.Server.Host != "irc.example.net" {
		t.Fatalf("%+v", cfg)
	}
	if got, want := cfg.Store.Path, filepath.Join(dir, "eggbot.db"); got != want {
		t.Fatalf("store path: got %q want %q (relative paths follow the toml)", got, want)
	}
	if got, want := cfg.Scripts.LuaDir, filepath.Join(dir, "scripts/lua"); got != want {
		t.Fatalf("lua dir: got %q want %q", got, want)
	}
	if _, ok := cfg.Channel["#cool"]; !ok {
		t.Fatal("missing channel")
	}
	if !cfg.IsOwnerHandle("nate") {
		t.Fatal("owner")
	}
}

func TestLoadKeepsAbsolutePaths(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "data", "bot.db")
	p := filepath.Join(dir, "c.toml")
	body := fmt.Sprintf(`
nick = "eggy"
[server]
host = "irc.example.net"
[store]
path = %q
`, db)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Store.Path != db {
		t.Fatalf("got %q want %q", cfg.Store.Path, db)
	}
}

func TestValidateRejectsNicknameOnlyAutoOwner(t *testing.T) {
	cfg := Defaults()
	cfg.Server.Host = "irc.example.net"
	cfg.Owners.Handles = []string{"nate"}
	cfg.Owners.AutoOwnerHosts = []string{"nate!*@*"}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected nickname-only owner mask to be rejected")
	}

	cfg.Owners.AutoOwnerHosts = []string{"nate!*@*.trusted.example"}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("specific owner mask rejected: %v", err)
	}

	cfg.Owners.AutoOwnerHosts = []string{"someone!*@*.trusted.example"}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected non-owner mask to be rejected")
	}
}

func TestValidateRejectsInsecureCredentialTransports(t *testing.T) {
	cfg := Defaults()
	cfg.Server.Host = "irc.example.net"
	cfg.Server.TLS = false
	cfg.Server.SASLAccount = "eggbot"
	cfg.Server.SASLPassword = "secret"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected SASL over plaintext IRC to be rejected")
	}

	cfg.Server.TLS = true
	cfg.Server.InsecureSkipVerify = true
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected SASL with certificate verification disabled to be rejected")
	}

	cfg.Server.SASLAccount = ""
	cfg.Server.SASLPassword = ""
	cfg.Server.InsecureSkipVerify = false
	cfg.Partyline.Listen = "0.0.0.0:3333"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected non-loopback plaintext partyline to be rejected")
	}
}

func TestServerAddrIPv6(t *testing.T) {
	cfg := Defaults()
	cfg.Server.Host = "2001:db8::1"
	cfg.Server.Port = 6697
	if got := cfg.ServerAddr(); got != "[2001:db8::1]:6697" {
		t.Fatal(got)
	}
}

func TestDiffHotAndCold(t *testing.T) {
	a := Defaults()
	b := Defaults()
	a.Server.Host = "irc.example.net"
	b.Server.Host = "irc.example.net"
	b.LLM.Model = "other"
	diff := Diff(a, b)
	hot := false
	for _, c := range diff {
		if c.Path == "llm.model" && c.Hot {
			hot = true
		}
		if c.Path == "server.host" && !c.Hot {
			t.Fatal("host should only appear when changed")
		}
	}
	if !hot {
		t.Fatal(diff)
	}
	b.Server.Host = "other.example.net"
	diff = Diff(a, b)
	cold := false
	for _, c := range diff {
		if c.Path == "server.host" && !c.Hot {
			cold = true
		}
	}
	if !cold {
		t.Fatal(diff)
	}
}

func TestValidateProtectsLLMKeyAndRawLogs(t *testing.T) {
	cfg := Defaults()
	cfg.Server.Host = "irc.example.net"
	cfg.LLM.APIKey = "test"
	cfg.LLM.BaseURL = "http://api.example.net/v1"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected non-loopback HTTP LLM endpoint to be rejected")
	}

	cfg.LLM.BaseURL = "http://127.0.0.1:8080/v1"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("loopback development endpoint rejected: %v", err)
	}

	cfg.Log.DebugIRC = true
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected raw IRC logging to be rejected")
	}
}
