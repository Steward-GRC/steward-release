// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	edgeSha = "0123456789abcdef0123456789abcdef01234567"
	edgeDig = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
)

// edgeManifest is a real (non-placeholder) two-store manifest: beta runs after alpha.
func edgeManifest(t *testing.T) string {
	t.Helper()
	doc := `{
  "schemaVersion": 1, "release": "0.1.0",
  "refs": {"core": "` + edgeSha + `", "proto": {"alpha": "` + edgeSha + `"}},
  "services": [
    {"name": "beta", "image": "ghcr.io/x/beta", "digest": "` + edgeDig + `", "store": "postgres", "migrateAfter": ["alpha"]},
    {"name": "alpha", "image": "ghcr.io/x/alpha", "digest": "` + edgeDig + `", "store": "postgres"},
    {"name": "edge", "image": "ghcr.io/x/edge", "digest": "` + edgeDig + `", "store": "none"}
  ]
}`
	p := filepath.Join(t.TempDir(), "manifest.json")
	if err := os.WriteFile(p, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func edgeMigrations(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, s := range []string{"alpha", "beta"} {
		if err := os.Mkdir(filepath.Join(root, s), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, s, "0001_init.up.sql"), []byte("select 1;"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestDryRunWithUnreadableDSNsReportsEveryStoreInOrder(t *testing.T) {
	out, err := run(t, "--manifest", edgeManifest(t), "--migrations-root", edgeMigrations(t))
	if err == nil {
		t.Fatal("an unreadable store blocks the plan")
	}
	a := strings.Index(out, "STEWARD_MIGRATE_DSN_ALPHA is not set")
	b := strings.Index(out, "STEWARD_MIGRATE_DSN_BETA is not set")
	if a < 0 || b < 0 || a > b {
		t.Fatalf("both stores should be reported, alpha first:\n%s", out)
	}
	if strings.Contains(out, "EDGE") {
		t.Fatalf("a store-less service has no DSN:\n%s", out)
	}
	if !strings.Contains(out, "steward-migrate dry-run") {
		t.Fatalf("not a dry-run:\n%s", out)
	}
}

func TestNoDSNOrPasswordReachesTheOutput(t *testing.T) {
	const pw = "s3cr3tpw"
	dsns := []string{
		"postgres://user:" + pw + "@127.0.0.1:1/db?sslmode=disable",
		"postgres://user:" + pw + "@127.0.0.1:1/db",
		"postgres://127.0.0.1:1/db?user=user&password=" + pw + "&sslmode=disable",
		"mysql://user:" + pw + "@127.0.0.1:1/db",
		"user:" + pw + "@127.0.0.1:1/db",
		"postgres://user:" + pw + "@127.0.0.1:badport/db",
	}
	for _, dsn := range dsns {
		for _, apply := range []bool{false, true} {
			t.Run(strings.ReplaceAll(dsn, pw, "***")+map[bool]string{false: "/dry", true: "/apply"}[apply], func(t *testing.T) {
				args := []string{"--manifest", edgeManifest(t), "--migrations-root", edgeMigrations(t)}
				if apply {
					args = append(args, "--apply")
				}
				// run clears the known names; set ours after it would, so wrap.
				t.Setenv("STEWARD_MIGRATE_DSN_ALPHA", dsn)
				t.Setenv("STEWARD_MIGRATE_DSN_BETA", dsn)
				out, err := runKeepEnv(t, args...)
				if err == nil {
					t.Fatalf("an unreachable store must fail:\n%s", out)
				}
				if strings.Contains(out, pw) || strings.Contains(out, dsn) {
					t.Fatalf("the DSN leaked:\n%s", out)
				}
			})
		}
	}
}

func TestApplyBlockedByOneUnreadableStoreRunsNothing(t *testing.T) {
	t.Setenv("STEWARD_MIGRATE_DSN_ALPHA", "postgres://u:pw@127.0.0.1:1/db?sslmode=disable")
	out, err := runKeepEnv(t, "--manifest", edgeManifest(t), "--migrations-root", edgeMigrations(t), "--apply")
	if err == nil {
		t.Fatal("expected a blocked apply")
	}
	if !strings.Contains(out, "STEWARD_MIGRATE_DSN_BETA is not set") {
		t.Fatalf("the unset store should be named:\n%s", out)
	}
	if strings.Contains(out, " applied") {
		t.Fatalf("nothing may be applied when a store is unreadable:\n%s", out)
	}
}

func TestStrictFlagRejectsPartlyFilledPlaceholdersInADryRun(t *testing.T) {
	zero := strings.Repeat("0", 40)
	doc := `{
  "schemaVersion": 1, "release": "0.1.0",
  "refs": {"core": "` + edgeSha + `", "proto": {"alpha": "` + zero + `"}},
  "services": [
    {"name": "alpha", "image": "ghcr.io/x/alpha", "digest": "` + edgeDig + `", "store": "postgres"}
  ]
}`
	p := filepath.Join(t.TempDir(), "m.json")
	if err := os.WriteFile(p, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, "--manifest", p, "--migrations-root", t.TempDir()); err == nil || errIsPlaceholder(err) {
		t.Fatalf("without --strict a dry-run reads the stores (and is blocked by the missing DSN), got %v", err)
	}
	out, err := run(t, "--manifest", p, "--migrations-root", t.TempDir(), "--strict")
	if err == nil || !strings.Contains(out, "refs.proto.alpha") || strings.Contains(out, "steward-migrate dry-run") {
		t.Fatalf("--strict must stop before planning and name the placeholder, got %v\n%s", err, out)
	}
}

func errIsPlaceholder(err error) bool { return strings.Contains(err.Error(), "placeholder") }

func TestBadManifestInputs(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	cases := map[string]string{
		"missing file": filepath.Join(dir, "absent.json"),
		"empty file":   write("empty.json", ""),
		"not json":     write("bad.json", "{"),
	}
	for name, p := range cases {
		t.Run(name, func(t *testing.T) {
			out, err := run(t, "--manifest", p, "--migrations-root", t.TempDir())
			if err == nil {
				t.Fatalf("expected an error:\n%s", out)
			}
			if strings.Contains(out, "steward-migrate dry-run") {
				t.Fatalf("a bad manifest must stop before planning:\n%s", out)
			}
		})
	}
}

func cycleDoc() string {
	return `{
  "schemaVersion": 1, "release": "0.1.0",
  "refs": {"core": "` + edgeSha + `", "proto": {"a": "` + edgeSha + `"}},
  "services": [
    {"name": "a", "image": "ghcr.io/x/a", "digest": "` + edgeDig + `", "store": "postgres", "migrateAfter": ["b"]},
    {"name": "b", "image": "ghcr.io/x/b", "digest": "` + edgeDig + `", "store": "postgres", "migrateAfter": ["a"]}
  ]
}`
}

func TestCycleIsReportedBeforeAnyStoreIsRead(t *testing.T) {
	p := filepath.Join(t.TempDir(), "cycle.json")
	if err := os.WriteFile(p, []byte(cycleDoc()), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := run(t, "--manifest", p, "--migrations-root", t.TempDir())
	if err == nil || !strings.Contains(out, "cycle") {
		t.Fatalf("want a cycle error, got %v\n%s", err, out)
	}
	if strings.Contains(out, "is not set") {
		t.Fatalf("no store should have been read:\n%s", out)
	}
}

func TestRejectsArgsAndUnknownFlags(t *testing.T) {
	if _, err := run(t, "extra"); err == nil {
		t.Fatal("positional arguments are not accepted")
	}
	if _, err := run(t, "--dsn", "postgres://u:p@h/db"); err == nil {
		t.Fatal("a DSN flag must not exist: DSNs come from the environment only")
	}
}

// runKeepEnv is run without clearing the DSN variables, for tests that set them.
func runKeepEnv(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	cmd := newRoot()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}
