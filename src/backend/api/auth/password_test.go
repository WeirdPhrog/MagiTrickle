package auth

import (
	"strings"
	"testing"
)

func TestOversizedCredentialsAreRejectedBeforeHashing(t *testing.T) {
	password := strings.Repeat("a", maxPasswordBytes+1)
	if _, err := Authenticate("root", password); err == nil || err.Error() != "credentials too long" {
		t.Fatalf("got %v", err)
	}
	if _, err := Authenticate(strings.Repeat("a", maxLoginBytes+1), "secret"); err == nil || err.Error() != "credentials too long" {
		t.Fatalf("got %v", err)
	}
	if verifyPassword(password, "$6$salt$hash") {
		t.Fatal("oversized password accepted")
	}
}

func TestPasswordVerificationStillMatchesSHA512Crypt(t *testing.T) {
	hash, err := cryptPassword("test-password", "$6$salt$")
	if err != nil {
		t.Fatal(err)
	}
	if !verifyPassword("test-password", hash) || verifyPassword("wrong", hash) {
		t.Fatal("password verification failed")
	}
}
