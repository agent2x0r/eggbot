package origin

import "testing"

func TestParse353Rustirc(t *testing.T) {
	ch, names, ok := Parse353([]string{"#lobby", "@chrisk nate eggbot"})
	if !ok || ch != "#lobby" || names != "@chrisk nate eggbot" {
		t.Fatalf("%q %q %v", ch, names, ok)
	}
	ch, names, ok = Parse353([]string{"eggbot", "=", "#lobby", "@nate eggbot"})
	if !ok || ch != "#lobby" || names != "@nate eggbot" {
		t.Fatalf("rfc %q %q %v", ch, names, ok)
	}
}

func TestParse332Rustirc(t *testing.T) {
	ch, topic, ok := Parse332([]string{"#lobby", "hello"})
	if !ok || ch != "#lobby" || topic != "hello" {
		t.Fatalf("%q %q %v", ch, topic, ok)
	}
}
