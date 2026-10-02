package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestCommandsRejectUnexpectedArgumentsBeforeSideEffects(t *testing.T) {
	for _, path := range [][]string{
		{"cache", "status"}, {"cache", "clear"}, {"job", "list"},
		{"dependency", "list"}, {"dependency", "install"}, {"dependency", "update"}, {"skill"},
	} {
		t.Run(strings.Join(path, "/"), func(t *testing.T) {
			cacheDir := t.TempDir()
			marker := filepath.Join(cacheDir, "saved.json")
			if err := os.WriteFile(marker, []byte("retained"), 0o600); err != nil {
				t.Fatal(err)
			}
			runner := func(context.Context, string, ...string) ([]byte, error) {
				t.Error("unexpected external program invocation")
				return []byte(ffmpegVersionOutput), nil
			}
			root, _, _ := newTestCLIWithRunner(t, cacheDir, "", runner)
			// Package-manager installation uses a non-injectable runner. Replace
			// only those handlers; exercise real validation before any RunE.
			if path[0] == "dependency" && path[1] != "list" {
				cmd, _, err := root.Find(path)
				if err != nil {
					t.Fatal(err)
				}
				cmd.RunE = func(*cobra.Command, []string) error {
					t.Error("installation reached before argument rejection")
					return nil
				}
			}
			root.SetArgs(append(path, "unexpected"))
			if err := root.Execute(); err == nil {
				t.Fatal("expected argument rejection")
			}
			if _, err := os.Stat(marker); err != nil {
				t.Fatalf("validation changed the cache: %v", err)
			}
		})
	}
}
