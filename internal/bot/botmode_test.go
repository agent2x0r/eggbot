package bot

import "testing"

func TestClaimBotModeOncePerConnection(t *testing.T) {
	b := testBot(t)

	if got := b.claimBotMode(); got != "" {
		t.Fatalf("claimed %q without BOT= advertised", got)
	}

	b.IRC.ISupport().ApplyTokens([]string{"BOT=B"})
	if got := b.claimBotMode(); got != "B" {
		t.Fatalf("claim = %q, want B", got)
	}
	if got := b.claimBotMode(); got != "" {
		t.Fatalf("second claim = %q, want latched", got)
	}

	// A new connection (001) re-arms the latch.
	b.onWelcome()
	b.mu.Lock()
	sent := b.botModeSent
	b.mu.Unlock()
	if !sent {
		t.Fatal("welcome with BOT= known did not claim")
	}
}

func TestClaimBotModeRejectsBadLetter(t *testing.T) {
	b := testBot(t)
	for _, v := range []string{"BOT=", "BOT=BB", "BOT=+", "BOT=1"} {
		b.IRC.ISupport().ApplyTokens([]string{v})
		if got := b.claimBotMode(); got != "" {
			t.Fatalf("%s: claimed %q", v, got)
		}
	}
}
