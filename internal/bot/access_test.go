package bot

import (
	"testing"
	"time"

	"eggbot/internal/flags"
	"eggbot/internal/userfile"
)

func TestOwnerCanJoin(t *testing.T) {
	u := &userfile.User{Handle: "nate", Global: flags.Parse("np")}
	if !canParty(u, "m") || !canParty(u, "o") || !canParty(u, "n") {
		t.Fatalf("owner +np should use master/op/owner commands, flags=%s", u.Global)
	}
	master := &userfile.User{Handle: "mod", Global: flags.Parse("m")}
	if !canParty(master, "m") || !canParty(master, "o") || canParty(master, "n") {
		t.Fatal("master should do m/o but not n")
	}
	nobody := &userfile.User{Handle: "x", Global: flags.Parse("p")}
	if canParty(nobody, "m") || canParty(nobody, "o") {
		t.Fatal("party-only should not master/op")
	}
}

func TestPrivilegeHierarchy(t *testing.T) {
	owner := &userfile.User{Handle: "owner", Global: flags.Parse("n")}
	otherOwner := &userfile.User{Handle: "owner2", Global: flags.Parse("n")}
	master := &userfile.User{Handle: "master", Global: flags.Parse("m")}
	op := &userfile.User{Handle: "op", Global: flags.Parse("o")}
	user := &userfile.User{Handle: "user", Global: flags.Parse("p")}

	if canAdminister(master, owner) || canAdminister(owner, otherOwner) {
		t.Fatal("equal-or-higher accounts must not be administrable")
	}
	if !canAdminister(master, op) || !canAdminister(owner, master) {
		t.Fatal("higher-ranked accounts should administer lower-ranked accounts")
	}
	if canChangeFlags(master, user, flags.Parse("n")) || canChangeFlags(master, user, flags.Parse("m")) {
		t.Fatal("master must not grant master or owner")
	}
	if !canChangeFlags(master, user, flags.Parse("o")) {
		t.Fatal("master should be able to grant op")
	}
	if !canChangeFlags(owner, master, flags.Parse("n")) {
		t.Fatal("owner should be able to promote a lower-ranked user to owner")
	}
}

func TestRedactQuery(t *testing.T) {
	if redactQuery("pass s3cret") != "pass *" || redactQuery("ident nate s3cret") != "ident *" {
		t.Fatal(redactQuery("pass s3cret"), redactQuery("ident nate s3cret"))
	}
	if redactQuery("hello") != "hello" {
		t.Fatal(redactQuery("hello"))
	}
}

func TestPublicCredentialCommand(t *testing.T) {
	for _, text := range []string{
		"eggbot: pass secret",
		"EggBot, IDENT owner secret",
		"!auth owner secret",
	} {
		if !publicCredentialCommand("eggbot", text) {
			t.Fatalf("expected credential command: %q", text)
		}
	}
	for _, text := range []string{
		"pass the salt",
		"eggbot: help",
		"!ask how do password managers work?",
	} {
		if publicCredentialCommand("eggbot", text) {
			t.Fatalf("unexpected credential command: %q", text)
		}
	}
}

func TestAuthAttemptLimit(t *testing.T) {
	b := &Bot{}
	now := time.Now()
	for i := 0; i < 5; i++ {
		if !b.allowAuthAttempt("nick!user@host", now.Add(time.Duration(i)*time.Second)) {
			t.Fatalf("attempt %d unexpectedly blocked", i+1)
		}
	}
	if b.allowAuthAttempt("nick!user@host", now.Add(10*time.Second)) {
		t.Fatal("sixth attempt should be blocked")
	}
	if !b.allowAuthAttempt("nick!user@host", now.Add(2*time.Minute)) {
		t.Fatal("attempt should be allowed after the window")
	}
}
