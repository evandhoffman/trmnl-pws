package main

import (
	"bytes"
	"testing"
)

func TestVersionFlag(t *testing.T) {
	var out bytes.Buffer
	if code := run([]string{"--version"}, &out); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if got := out.String(); got != "trmnl-poller dev\n" {
		t.Fatalf("got %q", got)
	}
}

func TestSubcommandStubs(t *testing.T) {
	tests := []struct {
		name string
		args []string
		code int
	}{
		{"no args behaves as serve", nil, 2},
		{"serve", []string{"serve"}, 2},
		{"healthcheck", []string{"healthcheck"}, 2},
		{"unknown subcommand", []string{"bogus"}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			if code := run(tt.args, &out); code != tt.code {
				t.Fatalf("exit %d, want %d", code, tt.code)
			}
			if out.Len() == 0 {
				t.Fatal("no message written")
			}
		})
	}
}

func TestStubMessagesNameTheCommand(t *testing.T) {
	for _, cmd := range []string{"serve", "healthcheck"} {
		var out bytes.Buffer
		run([]string{cmd}, &out)
		want := cmd + ": not implemented\n"
		if got := out.String(); got != want {
			t.Fatalf("%s: got %q, want %q", cmd, got, want)
		}
	}
	var out bytes.Buffer
	run(nil, &out)
	if got := out.String(); got != "serve: not implemented\n" {
		t.Fatalf("no args: got %q", got)
	}
}
