// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package manifest

import (
	"strings"
	"testing"
)

const sha = "0123456789abcdef0123456789abcdef01234567"
const dig = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func valid() string {
	return `{
  "schemaVersion": 1,
  "release": "0.1.0",
  "refs": {"core": "` + sha + `", "proto": {"audit": "` + sha + `"}},
  "services": [
    {"name": "audit", "image": "ghcr.io/steward-grc/steward-audit", "digest": "` + dig + `", "store": "postgres"},
    {"name": "core", "image": "ghcr.io/steward-grc/steward-core", "digest": "` + dig + `", "store": "postgres", "migrateAfter": ["audit"]},
    {"name": "gateway", "image": "ghcr.io/steward-grc/steward-gateway", "digest": "` + dig + `", "store": "none"}
  ]
}`
}

func TestParseAcceptsValidManifest(t *testing.T) {
	m, err := Parse([]byte(valid()), Options{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(m.Services) != 3 || m.Services[1].MigrateAfter[0] != "audit" {
		t.Fatalf("unexpected manifest: %+v", m)
	}
}

func TestParseRejects(t *testing.T) {
	cases := []struct {
		name string
		edit func(string) string
		want string
	}{
		{"not json", func(string) string { return "{" }, "parse"},
		{"unknown field", func(s string) string {
			return strings.Replace(s, `"schemaVersion": 1,`, `"schemaVersion": 1, "extra": true,`, 1)
		}, "extra"},
		{"tag instead of digest", func(s string) string { return strings.Replace(s, dig, "latest", 1) }, "digest"},
		{"short digest", func(s string) string { return strings.Replace(s, dig, "sha256:abc", 1) }, "digest"},
		{"image with a tag", func(s string) string {
			return strings.Replace(s, "steward-audit\"", "steward-audit:v1\"", 1)
		}, "image"},
		{"short ref", func(s string) string { return strings.Replace(s, `"core": "`+sha+`"`, `"core": "abc"`, 1) }, "core"},
		{"bad store", func(s string) string { return strings.Replace(s, `"store": "none"`, `"store": "mysql"`, 1) }, "store"},
		{"duplicate service", func(s string) string {
			return strings.Replace(s, `"name": "gateway"`, `"name": "audit"`, 1)
		}, "duplicate service"},
		{"proto ref for unknown service", func(s string) string {
			return strings.Replace(s, `"proto": {"audit"`, `"proto": {"nope"`, 1)
		}, "unknown service"},
		{"migrateAfter unknown", func(s string) string {
			return strings.Replace(s, `["audit"]`, `["nope"]`, 1)
		}, "unknown service"},
		{"migrateAfter a store-less service", func(s string) string {
			return strings.Replace(s, `["audit"]`, `["gateway"]`, 1)
		}, "no store"},
		{"migrateAfter on a store-less service", func(s string) string {
			return strings.Replace(s, `"store": "none"`, `"store": "none", "migrateAfter": ["audit"]`, 1)
		}, "no store"},
		{"migrateAfter itself", func(s string) string {
			return strings.Replace(s, `["audit"]`, `["core"]`, 1)
		}, "itself"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.edit(valid())), Options{})
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

func TestStrictRejectsPlaceholders(t *testing.T) {
	zeroDig := "sha256:" + strings.Repeat("0", 64)
	zeroSha := strings.Repeat("0", 40)
	cases := map[string]string{
		"digest":  strings.Replace(valid(), dig, zeroDig, 1),
		"core":    strings.Replace(valid(), `"core": "`+sha+`"`, `"core": "`+zeroSha+`"`, 1),
		"proto":   strings.Replace(valid(), `"audit": "`+sha+`"`, `"audit": "`+zeroSha+`"`, 1),
		"release": strings.Replace(valid(), `"0.1.0"`, `"0.0.0-placeholder"`, 1),
	}
	for name, doc := range cases {
		if _, err := Parse([]byte(doc), Options{}); err != nil {
			t.Fatalf("%s: a placeholder must pass in the default mode: %v", name, err)
		}
		_, err := Parse([]byte(doc), Options{Strict: true})
		if err == nil || !strings.Contains(err.Error(), "placeholder") {
			t.Fatalf("%s: strict mode must reject the placeholder, got %v", name, err)
		}
	}
	if _, err := Parse([]byte(valid()), Options{Strict: true}); err != nil {
		t.Fatalf("a real manifest must pass strict mode: %v", err)
	}
}

func TestCommittedManifest(t *testing.T) {
	m, err := Load("../../release-manifest.json", Options{})
	if err != nil {
		t.Fatalf("committed manifest: %v", err)
	}
	if _, err := Load("../../release-manifest.json", Options{Strict: true}); err == nil {
		t.Fatal("the committed template must carry placeholders that strict mode rejects")
	}
	want := []string{"identity", "core", "workflow", "obligations", "audit", "delivery", "pdf-renderer", "collab",
		"ai", "ai-operator", "reporting", "gateway", "web-staff", "web-admin"}
	if len(m.Services) != len(want) {
		t.Fatalf("manifest lists %d services, values.yaml has %d", len(m.Services), len(want))
	}
	for i, n := range want {
		if m.Services[i].Name != n {
			t.Fatalf("service %d is %q, want %q", i, m.Services[i].Name, n)
		}
	}
}
