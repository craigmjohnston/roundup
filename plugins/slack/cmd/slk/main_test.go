package main

import (
	"strconv"
	"testing"
)

func TestParseParams(t *testing.T) {
	params, err := parseParams([]string{"filter=saved", "limit=5"})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "saved", params.Get("filter"))
	assertEqual(t, "5", params.Get("limit"))

	if _, err := parseParams([]string{"nope"}); err == nil {
		t.Error("want an error for a parameter without =, got none")
	}
}

func assertEqual(t *testing.T, want, got string) {
	t.Helper()
	if want != got {
		t.Errorf("want %q, got %q", want, got)
	}
}

func itoa(n int) string { return strconv.Itoa(n) }
