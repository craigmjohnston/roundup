package main

import (
	"context"
	"io"
	"strings"
	"testing"
)

func TestRun_RejectsBadArguments(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "when there is no source", args: nil, want: "usage"},
		{name: "when there is more than one source", args: []string{"reviews", "authored"}, want: "usage"},
		{name: "when the source is unknown", args: []string{"issues"}, want: `unknown source "issues"`},
		{name: "when a flag is unknown", args: []string{"-nope", "reviews"}, want: "flag provided but not defined"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := run(context.Background(), tc.args, strings.NewReader(""), io.Discard)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("want an error containing %q, got %v", tc.want, err)
			}
		})
	}
}
