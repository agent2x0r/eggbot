package irccase

import "testing"

func TestFoldRFC1459(t *testing.T) {
	if Fold("Nick[A]") != "nick{a}" {
		t.Fatalf("got %q", Fold("Nick[A]"))
	}
	if !Equal("FOO]", "foo}") {
		t.Fatal("expected equal")
	}
	if ASCII.Fold("Nick[A]") != "nick[a]" {
		t.Fatalf("ascii fold %q", ASCII.Fold("Nick[A]"))
	}
}

func TestStrictRFC1459(t *testing.T) {
	if StrictRFC1459.Fold("Nick[A]~") != "nick{a}~" {
		t.Fatalf("got %q", StrictRFC1459.Fold("Nick[A]~"))
	}
	if RFC1459.Fold("Nick~") != "nick^" {
		t.Fatalf("rfc1459 tilde %q", RFC1459.Fold("Nick~"))
	}
}

func TestParseMapper(t *testing.T) {
	if Parse("ascii") != ASCII || Parse("strict-rfc1459") != StrictRFC1459 || Parse("") != RFC1459 {
		t.Fatal("parse")
	}
	if ASCII.Name() != "ascii" {
		t.Fatal(ASCII.Name())
	}
}

func TestHandleFoldStable(t *testing.T) {
	if HandleFold("Owner") != Fold("Owner") {
		t.Fatal(HandleFold("Owner"))
	}
}

func FuzzFold(f *testing.F) {
	f.Add("Nick[A]~")
	f.Fuzz(func(t *testing.T, s string) {
		_ = RFC1459.Fold(s)
		_ = StrictRFC1459.Fold(s)
		_ = ASCII.Fold(s)
		_ = HandleFold(s)
	})
}
