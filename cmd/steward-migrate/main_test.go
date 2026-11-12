// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"strings"
	"testing"
)

func run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	for _, k := range []string{"CORE", "AUDIT", "IDENTITY"} {
		t.Setenv("STEWARD_MIGRATE_DSN_"+k, "")
	}
	var out bytes.Buffer
	cmd := newRoot()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestDefaultIsADryRun(t *testing.T) {
	out, err := run(t, "--manifest", "../../release-manifest.json")
	if err == nil {
		t.Fatal("with no DSNs set the plan is blocked")
	}
	if !strings.Contains(out, "steward-migrate dry-run") || strings.Contains(out, "steward-migrate apply") {
		t.Fatalf("not a dry-run:\n%s", out)
	}
	if !strings.Contains(out, "STEWARD_MIGRATE_DSN_AUDIT is not set") {
		t.Fatalf("the plan should say which DSN is missing:\n%s", out)
	}
}

func TestApplyRefusesAPlaceholderManifest(t *testing.T) {
	out, err := run(t, "--manifest", "../../release-manifest.json", "--apply")
	if err == nil || !strings.Contains(out, "placeholder") {
		t.Fatalf("want a placeholder error, got %v\n%s", err, out)
	}
	if strings.Contains(out, "steward-migrate apply") {
		t.Fatalf("apply started:\n%s", out)
	}
}
