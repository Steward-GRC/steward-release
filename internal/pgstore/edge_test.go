// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package pgstore

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnvNameTable(t *testing.T) {
	cases := map[string]string{
		"core":         "STEWARD_MIGRATE_DSN_CORE",
		"ai-operator":  "STEWARD_MIGRATE_DSN_AI_OPERATOR",
		"web-staff":    "STEWARD_MIGRATE_DSN_WEB_STAFF",
		"pdf-renderer": "STEWARD_MIGRATE_DSN_PDF_RENDERER",
		"a1-b2-c3":     "STEWARD_MIGRATE_DSN_A1_B2_C3",
	}
	for in, want := range cases {
		if got := EnvName(in); got != want {
			t.Errorf("EnvName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEachServiceReadsItsOwnVariable(t *testing.T) {
	asked := map[string]bool{}
	s := &Store{Root: t.TempDir(), Getenv: func(k string) string { asked[k] = true; return "" }}
	for _, svc := range []string{"core", "audit", "ai-operator"} {
		if _, err := s.Status(context.Background(), svc); err == nil || !strings.Contains(err.Error(), EnvName(svc)) {
			t.Fatalf("%s: want an error naming %s, got %v", svc, EnvName(svc), err)
		}
	}
	if len(asked) != 3 {
		t.Fatalf("each service must read its own variable, asked %v", asked)
	}
}

func TestApplyNeedsADSN(t *testing.T) {
	s := &Store{Root: t.TempDir(), Getenv: func(string) string { return "" }}
	err := s.Apply(context.Background(), "core")
	if err == nil || !strings.Contains(err.Error(), "STEWARD_MIGRATE_DSN_CORE is not set") {
		t.Fatalf("want an error naming the variable, got %v", err)
	}
}

func TestStatusReadsTheServicesOwnFolder(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "audit"), 0o700); err != nil {
		t.Fatal(err)
	}
	s := &Store{Root: root, Getenv: func(string) string { return "postgres://u:pw@127.0.0.1:1/db?sslmode=disable" }}
	// core has no folder: the error must come from the missing migrations
	// rather than from a connection, and must not reach for audit's folder.
	_, err := s.Status(context.Background(), "core")
	if err == nil {
		t.Fatal("a service without a migrations folder must fail")
	}
	if strings.Contains(err.Error(), "pw") {
		t.Fatalf("the password leaked: %v", err)
	}
}

// secrets are DSNs whose password must never appear in any error, in any form
// the driver might echo it.
var secrets = map[string]struct{ dsn, password string }{
	"plain":            {"postgres://user:hunter2@127.0.0.1:1/db?sslmode=disable", "hunter2"},
	"no sslmode":       {"postgres://user:hunter2@127.0.0.1:1/db", "hunter2"},
	"escaped password": {"postgres://user:p%40ss%2Fw0rd@127.0.0.1:1/db?sslmode=disable", "p%40ss%2Fw0rd"},
	"query password":   {"postgres://127.0.0.1:1/db?user=user&password=hunter2&sslmode=disable", "hunter2"},
	"bad scheme":       {"mysql://user:hunter2@127.0.0.1:1/db", "hunter2"},
	"no scheme":        {"user:hunter2@127.0.0.1:1/db", "hunter2"},
	"bad port":         {"postgres://user:hunter2@127.0.0.1:notaport/db", "hunter2"},
	"bad escape":       {"postgres://user:hunter2@127.0.0.1:1/db?x=%zz", "hunter2"},
	"whitespace":       {"  postgres://user:hunter2@127.0.0.1:1/db?sslmode=disable  ", "hunter2"},
}

func TestNoErrorPathEchoesTheDSNOrItsPassword(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "core"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "core", "0001_init.up.sql"), []byte("select 1;"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, tc := range secrets {
		t.Run(name, func(t *testing.T) {
			s := &Store{Root: root, Getenv: func(string) string { return tc.dsn }}
			_, statusErr := s.Status(context.Background(), "core")
			applyErr := s.Apply(context.Background(), "core")
			for label, err := range map[string]error{"Status": statusErr, "Apply": applyErr} {
				if err == nil {
					t.Errorf("%s: want an error", label)
					continue
				}
				msg := err.Error()
				if strings.Contains(msg, tc.password) || strings.Contains(msg, strings.TrimSpace(tc.dsn)) {
					t.Errorf("%s leaked the DSN: %s", label, msg)
				}
			}
		})
	}
}

func TestRedact(t *testing.T) {
	cases := []struct {
		in, dsn, want string
	}{
		{"dial postgres://u:p@h/db: refused", "postgres://u:p@h/db", "dial <dsn>: refused"},
		{"twice a://x a://x", "a://x", "twice <dsn> <dsn>"},
		{"no secret here", "a://x", "no secret here"},
	}
	for _, tc := range cases {
		if got := redact(errString(tc.in), tc.dsn); got != tc.want {
			t.Errorf("redact(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

type errString string

func (e errString) Error() string { return string(e) }
