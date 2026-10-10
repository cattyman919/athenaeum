// Package checkdigit validates EAN-13 / ISBN-13 check digits (docs/11).
package checkdigit

// EAN13 returns the check digit for the first 12 digits of an EAN-13 code.
// ok is false when digits is not exactly 12 ASCII digits.
func EAN13(digits string) (check int, ok bool) {
	if len(digits) != 12 {
		return 0, false
	}
	sum := 0
	for i := range len(digits) {
		c := digits[i]
		if c < '0' || c > '9' {
			return 0, false
		}
		d := int(c - '0')
		if i%2 == 1 {
			d *= 3
		}
		sum += d
	}
	return (10 - sum%10) % 10, true
}

// ValidEAN13 reports whether s is 13 digits with a correct check digit.
func ValidEAN13(s string) bool {
	if len(s) != 13 {
		return false
	}
	want, ok := EAN13(s[:12])
	return ok && s[12] >= '0' && s[12] <= '9' && int(s[12]-'0') == want
}

// ValidISBN13 reports whether s is an EAN-13 with a 978 or 979 prefix.
func ValidISBN13(s string) bool {
	return ValidEAN13(s) && (s[:3] == "978" || s[:3] == "979")
}
