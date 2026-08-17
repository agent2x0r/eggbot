package app

import "testing"

func TestParseLevel(t *testing.T) {
	if parseLevel("error") == parseLevel("info") {
		t.Fatal("levels")
	}
}

func TestMainVersion(t *testing.T) {
	if code := Main([]string{"-version"}); code != 0 {
		t.Fatalf("version exit %d", code)
	}
}
