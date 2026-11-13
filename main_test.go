package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"os"
	"strings"
	"testing"
)

func TestResolveStaysInsideRemoteDir(t *testing.T) {
	tests := []struct {
		remoteDir, name, want string
	}{
		{"/srv/data", "/", "/srv/data"},
		{"/srv/data", "/a/b.txt", "/srv/data/a/b.txt"},
		{"/srv/data", "/../../etc/passwd", "/srv/data/etc/passwd"},
		{"/srv/data", "a/../../x", "/srv/data/x"},
		{".", "/", "."},
		{".", "/../x", "x"},
	}
	for _, tt := range tests {
		fsys := &SFTPFileSystem{remoteDir: tt.remoteDir}
		got := fsys.resolve(tt.name)
		if got != tt.want {
			t.Errorf("resolve(%q) in %q = %q, want %q", tt.name, tt.remoteDir, got, tt.want)
		}
	}
}

func TestRemoveAllRefusesRoot(t *testing.T) {
	fsys := &SFTPFileSystem{remoteDir: "/srv/data"}
	for _, name := range []string{"/", "", "/..", "/a/../"} {
		err := fsys.RemoveAll(context.Background(), name)
		if !errors.Is(err, os.ErrInvalid) {
			t.Errorf("RemoveAll(%q) = %v, want os.ErrInvalid", name, err)
		}
	}
}

func TestHelpGoesToStdout(t *testing.T) {
	var out bytes.Buffer
	err := run([]string{"-h"}, &out)
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("run(-h) = %v, want flag.ErrHelp", err)
	}
	if !strings.HasPrefix(out.String(), "usage: sftpdav") {
		t.Errorf("usage not on stdout: %q", out.String())
	}
}

func TestHostRequired(t *testing.T) {
	var out bytes.Buffer
	err := run(nil, &out)
	if err == nil || out.Len() != 0 {
		t.Errorf("run() = %v, stdout %q; want an error and nothing on stdout", err, out.String())
	}
}
