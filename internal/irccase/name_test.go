package irccase

import "testing"

func TestParseNameAndEqual(t *testing.T) {
	if Parse("ascii").Name() != "ascii" {
		t.Fatal("ascii")
	}
	if Parse("strict-rfc1459").Name() != "strict-rfc1459" {
		t.Fatal("strict")
	}
	if Parse("rfc1459").Name() != "rfc1459" {
		t.Fatal("rfc")
	}
	if !ASCII.Equal("A", "a") || RFC1459.Fold("[A]") != "{a}" {
		t.Fatal("fold")
	}
}
