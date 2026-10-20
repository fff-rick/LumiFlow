package auth

import "testing"

func TestPasswordHashAndVerify(t *testing.T) {
	hash, err := HashPassword("password123")
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword(hash, "password123") {
		t.Fatal("expected password to verify")
	}
	if VerifyPassword(hash, "wrong-password") {
		t.Fatal("wrong password verified")
	}
}

func TestLimitRunesPreservesUnicodeBoundary(t *testing.T) {
	value := "直播互动用户"
	if got := limitRunes(value, 4); got != "直播互动" {
		t.Fatalf("got %q", got)
	}
}
