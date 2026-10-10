package hash

import (
	"errors"
	"strings"
	"testing"
)

func TestHashVerifyRoundTrip(t *testing.T) {
	enc, err := Hash("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(enc, "$argon2id$v=19$m=65536,t=3,p=2$") {
		t.Fatalf("unexpected PHC prefix: %s", enc)
	}
	ok, err := Verify("correct horse battery", enc)
	if err != nil || !ok {
		t.Fatalf("verify good: %v %v", ok, err)
	}
	ok, err = Verify("wrong", enc)
	if err != nil || ok {
		t.Fatalf("verify bad: %v %v", ok, err)
	}
}

func TestHashSaltsAreUnique(t *testing.T) {
	a, _ := Hash("same")
	b, _ := Hash("same")
	if a == b {
		t.Fatal("hashes must differ by salt")
	}
}

func TestVerifyRejectsMalformed(t *testing.T) {
	tests := []string{
		"",
		"plaintext",
		"$argon2i$v=19$m=65536,t=3,p=2$c2FsdA$aGFzaA",
		"$argon2id$v=18$m=65536,t=3,p=2$c2FsdA$aGFzaA",
		"$argon2id$v=19$m=0,t=3,p=2$c2FsdA$aGFzaA",
		"$argon2id$v=19$m=65536,t=3,p=2$!!!$aGFzaA",
		"$argon2id$v=19$m=65536,t=3,p=2$c2FsdA",
	}
	for _, enc := range tests {
		ok, err := Verify("x", enc)
		if ok || !errors.Is(err, ErrInvalidHash) {
			t.Errorf("%q: ok=%v err=%v", enc, ok, err)
		}
	}
}
