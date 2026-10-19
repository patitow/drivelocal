package main

import "testing"

func TestIsIntroChapter(t *testing.T) {
	for title, want := range map[string]bool{
		"OP": true, "Opening": true, "Intro": true, "op 2": true, "Abertura": true, "Opening Song": true,
		"Chapter 1": false, "Prologue": false, "ED": false, "Part A": false, "": false,
	} {
		if got := isIntroChapter(Chapter{Start: 90, End: 180, Title: title}); got != want {
			t.Errorf("%q: %v, queria %v", title, got, want)
		}
	}
	if isIntroChapter(Chapter{Start: 0, End: 600, Title: "OP"}) {
		t.Error("capítulo de 10 min não é abertura")
	}
}
