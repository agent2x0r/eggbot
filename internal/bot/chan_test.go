package bot

import "testing"

func TestNormalizeChan(t *testing.T) {
	ch, err := normalizeChan("lobby")
	if err != nil || ch != "#lobby" {
		t.Fatalf("%q %v", ch, err)
	}
	ch, err = normalizeChan("#Lobbytest extra")
	if err != nil || ch != "#Lobbytest" {
		t.Fatalf("%q %v", ch, err)
	}
	if _, err := normalizeChan(""); err == nil {
		t.Fatal("expected error")
	}
}

func TestNewChannelDefaultsDoNotEnableAI(t *testing.T) {
	cs := defaultNewChanSet()
	if !cs.Has("seen") {
		t.Fatal("new channels should retain seen by default")
	}
	if cs.Has("ai") || cs.Has("aitools") {
		t.Fatal("AI capabilities must require an explicit chanset")
	}
}
