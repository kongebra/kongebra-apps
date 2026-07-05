package main

import (
	"strings"
	"testing"
)

func TestValidateSlug(t *testing.T) {
	valid := []string{"ab", "inmeta", "tronder-leikan", "team-42", "a1-b2-c3", strings.Repeat("a", 63)}
	for _, s := range valid {
		if err := ValidateSlug(s); err != nil {
			t.Errorf("ValidateSlug(%q) = %v, vil ha nil", s, err)
		}
	}

	invalid := []string{
		"",                      // tom
		"a",                     // for kort
		"-leikan",               // ledende bindestrek
		"leikan-",               // etterfølgende bindestrek
		"tronder--leikan",       // dobbel bindestrek
		"TronderLeikan",         // store bokstaver
		"tronder leikan",        // mellomrom
		"tronder_leikan",        // understrek
		"trønder",               // ikke-ascii
		"a/b",                   // skråstrek
		strings.Repeat("a", 64), // for langt
	}
	for _, s := range invalid {
		if err := ValidateSlug(s); err == nil {
			t.Errorf("ValidateSlug(%q) = nil, vil ha feil", s)
		}
	}
}

func TestValidateName(t *testing.T) {
	if err := ValidateName("Inmeta Games"); err != nil {
		t.Errorf("gyldig navn avvist: %v", err)
	}
	if err := ValidateName(""); err == nil {
		t.Error("tomt navn godtatt")
	}
	if err := ValidateName(strings.Repeat("x", 201)); err == nil {
		t.Error("for langt navn godtatt")
	}
}
