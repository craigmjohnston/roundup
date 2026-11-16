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
		{name: "when there is no workspace", args: nil, want: "-workspace is required"},
		{name: "when the workspace is not a subdomain", args: []string{"-workspace", "acme.example.com"}, want: "is not a slack.com subdomain"},
		{name: "when a flag is unknown", args: []string{"-nope"}, want: "flag provided but not defined"},
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
