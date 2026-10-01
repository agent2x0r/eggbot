package chanlog

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"eggbot/internal/store"
)

func TestParseQuery(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	from, to, words := ParseQuery("last 3 weeks discussed topic about dogs", now)
	if to != now || now.Sub(from) != 21*24*time.Hour {
		t.Fatalf("last 3 weeks from=%s to=%s", from, to)
	}
	if strings.Join(words, " ") != "dogs" {
		t.Fatalf("words %v", words)
	}
	from, to, words = ParseQuery("3 weeks ago dogs", now)
	if to.After(now) {
		t.Fatal("to")
	}
	if !contains(words, "dogs") {
		t.Fatalf("words %v", words)
	}
	_ = from
}

func TestParseQueryDropsFiller(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	_, _, words := ParseQuery("do you remember how much storage i said I had? We talked about it the other day", now)
	if strings.Join(words, " ") != "storage" {
		t.Fatalf("words %v", words)
	}
}

func TestSearchFindsStorageMemory(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	m := New(st)
	now := time.Now()
	if err := m.Add("#lobby", "nate", "I've got my eye on some used 14TB SAS drives. Right now I only have 24 usable (+20TB usable in the synology)"); err != nil {
		t.Fatal(err)
	}
	if err := m.Add("#lobby", "nate", "right now my 24TB array (8x4TB) has drives in excess of 90k hours of runtime"); err != nil {
		t.Fatal(err)
	}
	if err := m.Add("#lobby", "nate", "!history do you remember how much storage i said I had? We talked about it the other day"); err != nil {
		t.Fatal(err)
	}
	if err := m.Add("#lobby", "bob", "unrelated cats"); err != nil {
		t.Fatal(err)
	}
	lines, err := m.Search("#lobby", "do you remember how much storage i said I had? We talked about it the other day", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) < 2 {
		t.Fatalf("want nate storage lines, got %+v", lines)
	}
	joined := Format(lines)
	if !strings.Contains(joined, "24 usable") || !strings.Contains(joined, "24TB") {
		t.Fatalf("missed nate storage: %s", joined)
	}
	if strings.Contains(joined, "!history") {
		t.Fatal("query echo")
	}
}

func TestSearchFilters(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	m := New(st)
	now := time.Now()
	if err := m.Add("#lobby", "nate", "we talked about dogs in the park"); err != nil {
		t.Fatal(err)
	}
	if err := m.Add("#lobby", "bob", "unrelated cats"); err != nil {
		t.Fatal(err)
	}
	lines, err := m.Search("#lobby", "dogs", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 || !strings.Contains(lines[0].Text, "dogs") {
		t.Fatalf("%+v", lines)
	}
	got := Format(lines)
	if !strings.Contains(got, "nate") {
		t.Fatal(got)
	}
}

func contains(ss []string, w string) bool {
	for _, s := range ss {
		if s == w {
			return true
		}
	}
	return false
}
