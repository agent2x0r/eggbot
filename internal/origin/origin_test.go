package origin

import "testing"

func TestAddressed(t *testing.T) {
	body, ok := Addressed("eggbot", "eggbot: hello there")
	if !ok || body != "hello there" {
		t.Fatalf("%q %v", body, ok)
	}
	body, ok = Addressed("eggbot", "Eggbot, hi")
	if !ok || body != "hi" {
		t.Fatalf("%q %v", body, ok)
	}
	if _, ok := Addressed("eggbot", "eggs are cool"); ok {
		t.Fatal("false positive")
	}
}

func TestAddressedTo(t *testing.T) {
	nick, body, ok := AddressedTo("alice: hello")
	if !ok || nick != "alice" || body != "hello" {
		t.Fatalf("%q %q %v", nick, body, ok)
	}
	nick, body, ok = AddressedTo("Bob, later")
	if !ok || nick != "Bob" || body != "later" {
		t.Fatalf("%q %q %v", nick, body, ok)
	}
	if _, _, ok := AddressedTo("what about france"); ok {
		t.Fatal("space should not count as addressing")
	}
	if _, _, ok := AddressedTo("eggs are cool"); ok {
		t.Fatal("no prefix")
	}
}

func TestBang(t *testing.T) {
	cmd, args, ok := Bang("!seen nate")
	if !ok || cmd != "seen" || args != "nate" {
		t.Fatalf("%s %s %v", cmd, args, ok)
	}
	cmd, args, ok = Bang("!ask\twhat's up")
	if !ok || cmd != "ask" || args != "what's up" {
		t.Fatalf("%q %q %v", cmd, args, ok)
	}
}

func TestSplitTokenUnicodeWhitespace(t *testing.T) {
	cmd, args := SplitToken("pass\tsecret")
	if cmd != "pass" || args != "secret" {
		t.Fatalf("%q %q", cmd, args)
	}
	cmd, args = SplitToken("  ident   nate\tsecret  ")
	if cmd != "ident" || args != "nate\tsecret" {
		t.Fatalf("%q %q", cmd, args)
	}
	cmd, args = SplitToken("hello")
	if cmd != "hello" || args != "" {
		t.Fatalf("%q %q", cmd, args)
	}
}

func TestCTCP(t *testing.T) {
	cmd, arg, ok := IsCTCP("\x01VERSION\x01")
	if !ok || cmd != "VERSION" || arg != "" {
		t.Fatal(cmd, arg, ok)
	}
	cmd, arg, ok = IsCTCP("\x01DCC CHAT chat 1 2\x01")
	if !ok || cmd != "DCC" || arg != "CHAT chat 1 2" {
		t.Fatal(cmd, arg, ok)
	}
}

func TestIsChannel(t *testing.T) {
	if !IsChannel("#foo") || IsChannel("nate") {
		t.Fatal("channel detect")
	}
}
