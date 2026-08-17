package pass

import "testing"

func TestHashVerify(t *testing.T) {
	h, err := Hash("s3cret")
	if err != nil {
		t.Fatal(err)
	}
	if !Verify("s3cret", h) {
		t.Fatal("should verify")
	}
	if Verify("wrong", h) {
		t.Fatal("should not verify")
	}
}

func TestDummyVerifyDoesNotPanic(t *testing.T) {
	DummyVerify("no-such-user")
}
