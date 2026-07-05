package main

import (
	"errors"
	"testing"
)

func strptr(s string) *string { return &s }

func TestPersonInputNormalize(t *testing.T) {
	t.Run("trimmer navn og tomme optionale -> nil", func(t *testing.T) {
		in := PersonInput{Name: "  Ada  ", Department: strptr("  "), AvatarURL: strptr(" ")}
		out, err := in.normalize()
		if err != nil {
			t.Fatalf("normalize: %v", err)
		}
		if out.Name != "Ada" {
			t.Errorf("name = %q, vil ha %q", out.Name, "Ada")
		}
		if out.Department != nil || out.AvatarURL != nil {
			t.Errorf("tomme optionale ble ikke nil: dep=%v avatar=%v", out.Department, out.AvatarURL)
		}
	})

	t.Run("beholder satte optionale trimmet", func(t *testing.T) {
		in := PersonInput{Name: "Ada", Department: strptr(" Eng "), AvatarURL: strptr("http://x/a.png")}
		out, err := in.normalize()
		if err != nil {
			t.Fatalf("normalize: %v", err)
		}
		if out.Department == nil || *out.Department != "Eng" {
			t.Errorf("department = %v, vil ha Eng", out.Department)
		}
		if out.AvatarURL == nil || *out.AvatarURL != "http://x/a.png" {
			t.Errorf("avatar = %v", out.AvatarURL)
		}
	})

	t.Run("tomt navn -> ErrInvalidInput", func(t *testing.T) {
		for _, name := range []string{"", "   ", "\t"} {
			if _, err := (PersonInput{Name: name}).normalize(); !errors.Is(err, ErrInvalidInput) {
				t.Errorf("name=%q: err=%v, vil ha ErrInvalidInput", name, err)
			}
		}
	})

	t.Run("for langt navn -> ErrInvalidInput", func(t *testing.T) {
		long := make([]byte, 201)
		for i := range long {
			long[i] = 'a'
		}
		if _, err := (PersonInput{Name: string(long)}).normalize(); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("langt navn: err=%v, vil ha ErrInvalidInput", err)
		}
	})
}

func TestValidateAccountID(t *testing.T) {
	if _, err := validateAccountID("  "); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("tom account_id: err=%v, vil ha ErrInvalidInput", err)
	}
	got, err := validateAccountID("  sub-123 ")
	if err != nil || got != "sub-123" {
		t.Errorf("validateAccountID = %q/%v, vil ha sub-123/nil", got, err)
	}
}
