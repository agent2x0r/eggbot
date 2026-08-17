package app

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"eggbot/internal/bot"
	"eggbot/internal/config"
	"eggbot/internal/obs"
)

func TestMainCheckConfigAndErrors(t *testing.T) {
	if code := Main([]string{"-bogus"}); code != 2 {
		t.Fatalf("bad flag %d", code)
	}
	if code := Main([]string{"-c", filepath.Join(t.TempDir(), "missing.toml")}); code != 1 {
		t.Fatalf("missing %d", code)
	}
	path := filepath.Join(t.TempDir(), "eggbot.toml")
	body := `
nick = "eggbot"
[server]
host = "irc.example.net"
tls = true
[owners]
handles = ["nate"]
[llm]
enabled = false
[store]
path = "` + filepath.Join(t.TempDir(), "t.db") + `"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := Main([]string{"-c", path, "-check-config"}); code != 0 {
		t.Fatal("check-config")
	}
	t.Setenv("EGGBOT_BOOTSTRAP_PASSWORD", "hunter22")
	if code := Main([]string{"-c", path, "-bootstrap-owner", "nate"}); code != 0 {
		t.Fatal("bootstrap")
	}
	if code := Main([]string{"-c", path, "-bootstrap-owner", "nobody"}); code != 1 {
		t.Fatal("bad owner")
	}
	_ = parseLevel("debug")
	_ = parseLevel("warn")
	_ = parseLevel("warning")
	_ = parseLevel("error")
	_ = parseLevel("info")
}

func TestAppRunShutsDown(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		_ = c.SetDeadline(time.Now().Add(15 * time.Second))
		r := bufio.NewReader(c)
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimSpace(line)
			switch {
			case strings.HasPrefix(line, "CAP LS"):
				_, _ = io.WriteString(c, ":test. CAP * LS :\r\n")
			case strings.HasPrefix(line, "CAP END"):
				_, _ = io.WriteString(c, ":test. 001 eggbot :welcome\r\n")
			}
		}
	}()

	cfg := config.Defaults()
	cfg.Nick = "eggbot"
	cfg.Server.Host = "127.0.0.1"
	cfg.Server.Port = ln.Addr().(*net.TCPAddr).Port
	cfg.Server.TLS = false
	cfg.LLM.Enabled = false
	cfg.Partyline.Listen = "127.0.0.1:0"
	cfg.Store.Path = filepath.Join(t.TempDir(), "t.db")
	cfg.Scripts.LuaDir = t.TempDir()
	cfg.Scripts.PythonDir = t.TempDir()
	cfg.Owners.Handles = []string{"nate"}
	log := slog.Default()
	b, err := bot.New(cfg, log)
	if err != nil {
		t.Fatal(err)
	}
	health := &obs.Server{Listen: "127.0.0.1:0", Log: log, Status: obs.Status{
		Live:  func() bool { return true },
		Ready: b.Ready,
	}}
	if err := health.Start(); err != nil {
		t.Fatal(err)
	}
	app := &App{Cfg: cfg, Log: log, Bot: b, Health: health}
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- app.Run(ctx) }()
	deadline := time.Now().Add(8 * time.Second)
	for !b.Ready() {
		select {
		case err := <-errc:
			t.Fatalf("run: %v", err)
		default:
			if time.Now().After(deadline) {
				cancel()
				t.Fatal("not ready")
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	cancel()
	select {
	case <-errc:
	case <-time.After(8 * time.Second):
		t.Fatal("App.Run did not return")
	}
}
