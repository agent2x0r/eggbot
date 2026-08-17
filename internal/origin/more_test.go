package origin

import "testing"

func TestParse366And332Variants(t *testing.T) {
	ch, ok := Parse366([]string{"eggbot", "#chan", "end"})
	if !ok || ch != "#chan" {
		t.Fatal(ch, ok)
	}
	if _, ok := Parse366([]string{"nope"}); ok {
		t.Fatal("expected miss")
	}
	ch, topic, ok := Parse332([]string{"#chan", "t"})
	if !ok || ch != "#chan" || topic != "t" {
		t.Fatal(ch, topic, ok)
	}
	ch, topic, ok = Parse332([]string{"eggbot", "#chan", "t"})
	if !ok || ch != "#chan" {
		t.Fatal(ch, topic, ok)
	}
	if _, _, ok := Parse332([]string{"x"}); ok {
		t.Fatal("miss")
	}
	ch, names, ok := Parse353([]string{"eggbot", "#chan", "@a +b"})
	if !ok || ch != "#chan" || names == "" {
		t.Fatal(ch, names, ok)
	}
	if _, _, ok := Parse353([]string{"x"}); ok {
		t.Fatal("353 miss")
	}
	_ = Origin{Nick: "n", User: "u", Host: "h"}.String()
}
