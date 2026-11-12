package main

import (
	"reflect"
	"testing"
)

func TestExtractEmails(t *testing.T) {
	text := "Budi Santoso <Budi@ugm.ac.id>, siti@mail.ugm.ac.id;\n" +
		"name,email\nAni,ani@ugm.ac.id.\n  budi@UGM.ac.id  (duplicate)\n" +
		"not an address, @broken, x@y\nsomeone@gmail.com"
	want := []string{"budi@ugm.ac.id", "siti@mail.ugm.ac.id", "ani@ugm.ac.id", "someone@gmail.com"}
	if got := extractEmails(text); !reflect.DeepEqual(got, want) {
		t.Fatalf("extractEmails = %v, want %v", got, want)
	}
	if got := extractEmails("nothing here"); len(got) != 0 {
		t.Fatalf("expected no emails, got %v", got)
	}
}

func TestEmailInDomain(t *testing.T) {
	cases := map[string]bool{
		"a@ugm.ac.id":                true,
		"a@mail.ugm.ac.id":           true,
		"a@evilugm.ac.id":            false,
		"a@ugm.ac.id.evil.com":       false,
		"a@gmail.com":                false,
		"@ugm.ac.id":                 false,
		"a@xugm.ac.id":               false,
		"a.b+tag@students.ugm.ac.id": true,
	}
	for email, want := range cases {
		if got := emailInDomain(email, "ugm.ac.id"); got != want {
			t.Errorf("emailInDomain(%q) = %v, want %v", email, got, want)
		}
	}
	if emailInDomain("a@ugm.ac.id", "") {
		t.Error("an empty configured domain must not match anything")
	}
}
