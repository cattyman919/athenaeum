package checkdigit

import "testing"

func TestValidEAN13(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"9780441013593", true}, // Dune
		{"9780060850524", true},
		{"9788100000021", true},
		{"4006381333931", true},
		{"9780441013594", false}, // wrong check digit
		{"978044101359", false},  // too short
		{"97804410135930", false},
		{"978044101359X", false},
		{"", false},
		{"abcdefghijklm", false},
	}
	for _, tt := range tests {
		if got := ValidEAN13(tt.in); got != tt.want {
			t.Errorf("ValidEAN13(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestValidISBN13Prefix(t *testing.T) {
	if ValidISBN13("4006381333931") {
		t.Fatal("non-978/979 EAN must not be an ISBN")
	}
	if !ValidISBN13("9780441013593") {
		t.Fatal("Dune ISBN must be valid")
	}
}

func TestEAN13CheckDigit(t *testing.T) {
	if d, ok := EAN13("978044101359"); !ok || d != 3 {
		t.Fatalf("got %d %v", d, ok)
	}
	if _, ok := EAN13("12"); ok {
		t.Fatal("short input must fail")
	}
}
