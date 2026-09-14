package password

import "testing"

func TestHashAndVerify(t *testing.T) {
	hash, err := Hash("a-long-test-password")
	if err != nil {
		t.Fatal(err)
	}
	if !Verify("a-long-test-password", hash) {
		t.Fatal("expected password to verify")
	}
	if Verify("wrong-password", hash) {
		t.Fatal("wrong password verified")
	}
}
