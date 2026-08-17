package llm

import "testing"

func TestParseArgsAndToStr(t *testing.T) {
	if len(ParseArgs("{")) != 0 {
		t.Fatal("bad json")
	}
	m := ParseArgs(`{"n":1,"s":"x"}`)
	if Str(m, "s") != "x" || Str(m, "n") == "" || Str(m, "z") != "" {
		t.Fatal(m)
	}
	if NeedsSearch("") || !NeedsSearch("what's the latest news today") {
		t.Fatal("search")
	}
	if parseRoute("SEARCH please") != RouteSearch || parseRoute("LOCAL") != RouteLocal {
		t.Fatal("route")
	}
}
