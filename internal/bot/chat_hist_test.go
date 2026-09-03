package bot

import (
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"eggbot/internal/config"
	"eggbot/internal/llm"
)

func TestChatHistSurvivesShortRestart(t *testing.T) {
	cfg := config.Defaults()
	cfg.Nick = "eggbot"
	cfg.Server.Host = "irc.example.net"
	cfg.Server.TLS = true
	cfg.LLM.Enabled = false
	cfg.Owners.Handles = []string{"nate"}
	cfg.Store.Path = filepath.Join(t.TempDir(), "t.db")
	cfg.Scripts.LuaDir = t.TempDir()
	cfg.Scripts.PythonDir = t.TempDir()

	b1, err := New(cfg, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	b1.LLM.Remember("#lobby", "nate", "we were talking about wal")
	b1.LLM.Remember("#lobby", "bob", "checkpoint it")
	b1.saveChatHist()
	b1.Close()

	b2, err := New(cfg, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(b2.Close)
	got := b2.LLM.Tail("#lobby", 10)
	if !strings.Contains(got, "we were talking about wal") || !strings.Contains(got, "checkpoint it") {
		t.Fatalf("short restart lost hist: %q", got)
	}
}

func TestChatHistDroppedWhenStale(t *testing.T) {
	cfg := config.Defaults()
	cfg.Nick = "eggbot"
	cfg.Server.Host = "irc.example.net"
	cfg.Server.TLS = true
	cfg.LLM.Enabled = false
	cfg.Owners.Handles = []string{"nate"}
	cfg.Store.Path = filepath.Join(t.TempDir(), "t.db")
	cfg.Scripts.LuaDir = t.TempDir()
	cfg.Scripts.PythonDir = t.TempDir()

	b1, err := New(cfg, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-llm.ChatHistMaxAge - time.Minute)
	b1.nowFn = func() time.Time { return past }
	b1.LLM.Remember("#lobby", "nate", "old chatter")
	b1.saveChatHist()
	b1.Close()

	b2, err := New(cfg, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(b2.Close)
	if got := b2.LLM.Tail("#lobby", 10); got != "" {
		t.Fatalf("stale hist restored: %q", got)
	}
}
