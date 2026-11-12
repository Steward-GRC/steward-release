// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package pgstore

import (
	"context"
	"strings"
	"testing"
)

func TestEnvName(t *testing.T) {
	if got := EnvName("pdf-renderer"); got != "STEWARD_MIGRATE_DSN_PDF_RENDERER" {
		t.Fatalf("got %s", got)
	}
}

func TestStatusNeedsADSNAndNeverEchoesIt(t *testing.T) {
	s := &Store{Root: t.TempDir(), Getenv: func(string) string { return "" }}
	if _, err := s.Status(context.Background(), "core"); err == nil || !strings.Contains(err.Error(), "STEWARD_MIGRATE_DSN_CORE") {
		t.Fatalf("want an error naming the variable, got %v", err)
	}
	const secret = "postgres://user:hunter2@192.0.2.1:1/db"
	s = &Store{Root: t.TempDir(), Getenv: func(string) string { return secret }}
	_, err := s.Status(context.Background(), "core")
	if err == nil || strings.Contains(err.Error(), "hunter2") {
		t.Fatalf("want an error without the password, got %v", err)
	}
}
