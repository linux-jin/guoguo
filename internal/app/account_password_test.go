package app

import (
	"strings"
	"testing"
)

func TestValidateAccountPasswordAllowsMemorableSecrets(t *testing.T) {
	if err := validateAccountPassword("abc123"); err != nil {
		t.Fatal(err)
	}
	if err := validateAccountPassword("mypass"); err != nil {
		t.Fatal(err)
	}
	if err := validateAccountPassword(""); err == nil {
		t.Fatal("empty password accepted")
	}
	if err := validateAccountPassword(strings.Repeat("a", 129)); err == nil {
		t.Fatal("oversized password accepted")
	}
}
