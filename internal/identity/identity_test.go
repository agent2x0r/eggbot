package identity

import (
	"path/filepath"
	"testing"

	"eggbot/internal/store"
	"eggbot/internal/userfile"
)

func TestAccountPreferredOverHost(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	users := userfile.New(st, nil)
	if err := users.Add("nate", ""); err != nil {
		t.Fatal(err)
	}
	reg := New(st)
	if err := reg.Bind("nate", "example.net", MechAccount, "nateacct"); err != nil {
		t.Fatal(err)
	}
	h, mech := reg.Resolve("example.net", "nateacct", "")
	if h != "nate" || mech != MechAccount {
		t.Fatalf("%s %s", h, mech)
	}
	if _, ok := reg.Lookup("example.net", MechAccount, "other"); ok {
		t.Fatal("unknown account")
	}
}

func TestCertFPBinding(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	users := userfile.New(st, nil)
	if err := users.Add("nate", ""); err != nil {
		t.Fatal(err)
	}
	reg := New(st)
	if err := reg.Bind("nate", "example.net", MechCertFP, "abc123"); err != nil {
		t.Fatal(err)
	}
	h, mech := reg.Resolve("example.net", "", "ABC123")
	if h != "nate" || mech != MechCertFP {
		t.Fatalf("%s %s", h, mech)
	}
}

func TestUnbindAndForHandle(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := userfile.New(st, nil).Add("nate", ""); err != nil {
		t.Fatal(err)
	}
	reg := New(st)
	if err := reg.Bind("", "example.net", MechHost, "x"); err == nil {
		t.Fatal("incomplete")
	}
	if err := reg.Bind("nate", "example.net", MechHost, "nate!u@h"); err != nil {
		t.Fatal(err)
	}
	if err := reg.Bind("nate", "example.net", MechAccount, "nateacct"); err != nil {
		t.Fatal(err)
	}
	got := reg.ForHandle("NATE")
	if len(got) != 2 {
		t.Fatalf("bindings %d", len(got))
	}
	if err := reg.Unbind("nate", "example.net", MechHost, "nate!u@h"); err != nil {
		t.Fatal(err)
	}
	if h, ok := reg.Lookup("example.net", MechHost, "nate!u@h"); ok || h != "" {
		t.Fatal("unbound")
	}
	if len(reg.ForHandle("nate")) != 1 {
		t.Fatal("remaining")
	}
}
