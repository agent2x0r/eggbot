package flags

import "testing"

func TestStrongerCombinedHasAll(t *testing.T) {
	if !StrongerOrEqual(nil, 0) || !StrongerOrEqual(Parse("n"), 'o') {
		t.Fatal("owner")
	}
	if !StrongerOrEqual(Parse("m"), 'o') || StrongerOrEqual(Parse("v"), 'n') {
		t.Fatal("master/voice")
	}
	if !hasAll(Parse("no"), "") || hasAll(Parse("o"), "n") {
		t.Fatal("hasAll")
	}
	out := Combined(Parse("p"), Parse("o"))
	if !out.Has(Op) || !out.Has(Party) {
		t.Fatal(out)
	}
	var nilSet Set
	nilSet.Add('n')
	if nilSet.Has('n') {
		t.Fatal("nil add")
	}
	s := Parse("n")
	if err := s.Apply("o|m"); err != nil {
		t.Fatal(err)
	}
	if err := s.Apply("+o -m |"); err != nil {
		t.Fatal(err)
	}
	if err := s.Apply("+9"); err == nil {
		t.Fatal("bad flag")
	}
	cs, _ := ParseChanSet("+seen")
	if !cs.Has("seen") {
		t.Fatal("has")
	}
}
