package queue

import (
	"strings"
	"testing"
	"time"
)

func TestSplit(t *testing.T) {
	parts := Split("hello world this is a test of wrapping", 12)
	if len(parts) < 3 {
		t.Fatalf("parts=%v", parts)
	}
	for _, p := range parts {
		if len(p) > 12 {
			t.Fatalf("oversize %q", p)
		}
	}
	if got := strings.Join(parts, " "); !strings.Contains(got, "hello") {
		t.Fatal(got)
	}
}

func TestQueueRejectsProtocolDelimiterInjection(t *testing.T) {
	var got []string
	q := New(Config{
		QuickBurst: 4,
		QuickEvery: time.Millisecond,
		ServEvery:  time.Millisecond,
		HelpEvery:  time.Millisecond,
		MaxLine:    80,
	}, func(s string) { got = append(got, s) })

	q.PushText(Serv, "PRIVMSG #safe :", "hello\r\nMODE #safe +o attacker\x00")
	q.drain(time.Now())

	if len(got) != 1 {
		t.Fatalf("expected one frame, got %q", got)
	}
	if strings.ContainsAny(got[0], "\r\n\x00") {
		t.Fatalf("protocol delimiter survived: %q", got[0])
	}
	if got[0] != "PRIVMSG #safe :hello  MODE #safe +o attacker" {
		t.Fatalf("unexpected sanitized frame: %q", got[0])
	}
}

func TestQueueCapsCompleteWireLine(t *testing.T) {
	const max = 32
	var got []string
	q := New(Config{
		QuickBurst: 4,
		QuickEvery: time.Millisecond,
		ServEvery:  time.Millisecond,
		HelpEvery:  time.Millisecond,
		MaxLine:    max,
	}, func(s string) { got = append(got, s) })

	q.PushText(Serv, "PRIVMSG #safe :", strings.Repeat("é", 40))
	q.drain(time.Now())

	if len(got) == 0 {
		t.Fatal("expected at least one frame")
	}
	for _, line := range got {
		if len(line) > max {
			t.Fatalf("oversize line (%d): %q", len(line), line)
		}
		if !strings.HasPrefix(line, "PRIVMSG #safe :") {
			t.Fatalf("missing prefix: %q", line)
		}
		if strings.ToValidUTF8(line, "") != line {
			t.Fatalf("invalid UTF-8: %q", line)
		}
	}
}

func TestQueueHasBoundedBacklog(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxItems = 3
	q := New(cfg, func(string) {})
	for i := 0; i < 10; i++ {
		q.Push(Help, "help")
	}
	if q.Len() != 3 {
		t.Fatalf("backlog = %d, want 3", q.Len())
	}
	q.Push(Quick, "urgent")
	if q.Len() != 3 || len(q.quick) != 1 || len(q.help) != 2 {
		t.Fatalf("quick did not displace low-priority item: quick=%d help=%d", len(q.quick), len(q.help))
	}
}

func TestQueueSizeRestartAndGeneration(t *testing.T) {
	q := New(DefaultConfig(), func(string) {})
	q.Push(Serv, "a")
	q.Push(Help, "b")
	q.Push(Quick, "c")
	if q.Size(Serv) == 0 || q.Size(Help) == 0 {
		t.Fatal("size")
	}
	q.SetMaxLine(80)
	q.SetGeneration(2)
	if q.Len() != 0 || q.Generation() != 2 {
		t.Fatal("generation flush")
	}
	q.Start()
	q.Stop()
	q.Restart()
	q.Push(Protocol, "PONG :x")
	q.Stop()
}

func TestQueuePacing(t *testing.T) {
	var got []string
	q := New(Config{
		QuickBurst: 2,
		QuickEvery: 20 * time.Millisecond,
		ServEvery:  20 * time.Millisecond,
		HelpEvery:  50 * time.Millisecond,
		MaxLine:    400,
	}, func(s string) { got = append(got, s) })
	q.Push(Help, "help1")
	q.Push(Quick, "q1")
	q.Push(Quick, "q2")
	q.Push(Serv, "s1")
	now := time.Now()
	q.drain(now)
	if len(got) < 2 || got[0] != "q1" {
		t.Fatalf("expected quick first, got %v", got)
	}
	q.drain(now.Add(time.Second))
	if len(got) < 4 {
		t.Fatalf("expected more drains, got %v", got)
	}
}
