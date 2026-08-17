package hostmask

import "testing"

func TestMatch(t *testing.T) {
	cases := []struct {
		mask, nuh string
		want      bool
	}{
		{"nate!*@*", "nate!u@h", true},
		{"*!*@*.example.com", "a!b@foo.example.com", true},
		{"*!*@*.example.com", "a!b@example.org", false},
		{"*?te!*@*", "nate!x@y", true},
		{"nate", "nate!x@y", true},
		{"*!user@host", "n!user@host", true},
		{"*!*@host", "n!u@host", true},
		{"", "a!b@c", false},
	}
	for _, c := range cases {
		if got := Match(c.mask, c.nuh); got != c.want {
			t.Errorf("Match(%q,%q)=%v want %v", c.mask, c.nuh, got, c.want)
		}
	}
}

func TestSpecificity(t *testing.T) {
	if Specificity("a!b@c") <= Specificity("*!*@*") {
		t.Fatal("expected more specific mask to score higher")
	}
}
