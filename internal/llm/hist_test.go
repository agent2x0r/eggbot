package llm

import (
	"strings"
	"testing"
	"time"

	"eggbot/internal/config"
)

func TestDumpRestoreHistWithinMaxAge(t *testing.T) {
	cfg := config.Defaults()
	cfg.LLM.Limits.HistoryLines = 80
	br := NewBrain(cfg, nil, nil, nil)
	t.Cleanup(br.Close)
	now := time.Unix(1_700_000_000, 0)
	br.Remember("#lobby", "nate", "hello")
	br.Remember("#Lobby", "bob", "hi nate")
	raw, err := br.DumpHist(now)
	if err != nil || len(raw) == 0 {
		t.Fatalf("dump %v %q", err, raw)
	}
	br2 := NewBrain(cfg, nil, nil, nil)
	t.Cleanup(br2.Close)
	n, err := br2.RestoreHist(raw, now.Add(2*time.Minute))
	if err != nil || n != 2 {
		t.Fatalf("restore %d %v", n, err)
	}
	got := br2.Tail("#lobby", 10)
	if !strings.Contains(got, "<nate> hello") || !strings.Contains(got, "<bob> hi nate") {
		t.Fatalf("tail %q", got)
	}
}

func TestRestoreHistRejectsStale(t *testing.T) {
	cfg := config.Defaults()
	br := NewBrain(cfg, nil, nil, nil)
	t.Cleanup(br.Close)
	now := time.Unix(1_700_000_000, 0)
	br.Remember("#c", "a", "x")
	raw, err := br.DumpHist(now)
	if err != nil {
		t.Fatal(err)
	}
	br2 := NewBrain(cfg, nil, nil, nil)
	t.Cleanup(br2.Close)
	n, err := br2.RestoreHist(raw, now.Add(ChatHistMaxAge+time.Second))
	if err != nil || n != 0 {
		t.Fatalf("stale %d %v", n, err)
	}
	if br2.Tail("#c", 10) != "" {
		t.Fatal("expected empty hist")
	}
}
