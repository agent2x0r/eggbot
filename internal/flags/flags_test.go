package flags

import "testing"

func TestApplyAndMatch(t *testing.T) {
	s := Parse("n")
	if !s.Has(Owner) {
		t.Fatal("expected owner")
	}
	if err := s.Apply("+mof-n"); err != nil {
		t.Fatal(err)
	}
	if s.Has(Owner) || !s.Has(Master) || !s.Has(Op) || !s.Has(Friend) {
		t.Fatalf("got %s", s.String())
	}
	if err := s.Apply("o"); err != nil {
		t.Fatal(err)
	}
	if s.String() != "o" {
		t.Fatalf("replace: %s", s.String())
	}

	g := Parse("n")
	ch := Parse("o")
	if !MatchAttr(g, ch, "n") {
		t.Fatal("owner should match n")
	}
	if !MatchAttr(g, ch, "o") {
		t.Fatal("channel o should match o")
	}
	if !MatchAttr(nil, nil, "-") {
		t.Fatal("anyone")
	}
	if MatchAttr(Parse("v"), nil, "n") {
		t.Fatal("voice is not owner")
	}
	if !MatchAttr(Parse("n"), Parse("o"), "n|o") {
		t.Fatal("n|o")
	}
}

func TestChanSet(t *testing.T) {
	cs, err := ParseChanSet("+enforcebans +protectops +ai")
	if err != nil {
		t.Fatal(err)
	}
	if !cs.Has("ai") || !cs.Has("enforcebans") {
		t.Fatal(cs)
	}
	if err := cs.Apply("-ai +aitools"); err != nil {
		t.Fatal(err)
	}
	if cs.Has("ai") || !cs.Has("aitools") {
		t.Fatal(cs)
	}
	if _, err := ParseChanSet("+notathing"); err == nil {
		t.Fatal("expected unknown chanset error")
	}
	if !KnownChanSet("seen") || KnownChanSet("nope") {
		t.Fatal("known")
	}
	if FoldHandle("Nate") != "nate" {
		t.Fatal("fold")
	}
	if Meaning('n') == "" || Meaning('Z') != "user-defined" || Meaning('1') != "reserved" {
		t.Fatal("meaning")
	}
}
