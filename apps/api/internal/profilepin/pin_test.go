package profilepin

import (
	"strings"
	"testing"
)

func TestValidRequiresExactlyFourASCIIDigits(t *testing.T) {
	for _, pin := range []string{"0000", "1234", "9876"} {
		if !Valid(pin) {
			t.Fatalf("Valid(%q) = false", pin)
		}
	}
	for _, pin := range []string{"", "123", "12345", " 1234", "1234 ", "12 4", "12a4", "１２３４"} {
		if Valid(pin) {
			t.Fatalf("Valid(%q) = true", pin)
		}
	}
}

func TestHashIsSaltedAndNeverContainsTheRawPIN(t *testing.T) {
	first, err := Hash("1234")
	if err != nil {
		t.Fatalf("first hash: %v", err)
	}
	second, err := Hash("1234")
	if err != nil {
		t.Fatalf("second hash: %v", err)
	}
	if first == second || strings.Contains(first, "1234") || strings.Contains(second, "1234") {
		t.Fatalf("hashes must be distinct and one-way: %q / %q", first, second)
	}
	if matched, err := Matches(first, "1234"); err != nil || !matched {
		t.Fatalf("correct PIN matched=%v err=%v", matched, err)
	}
	if matched, err := Matches(first, "4321"); err != nil || matched {
		t.Fatalf("wrong PIN matched=%v err=%v", matched, err)
	}
}

func TestHashRejectsInvalidPIN(t *testing.T) {
	if _, err := Hash("12345"); err != ErrInvalid {
		t.Fatalf("Hash invalid error = %v, want %v", err, ErrInvalid)
	}
}
