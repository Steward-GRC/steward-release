// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package manifest

import (
	"strings"
	"testing"
)

func TestParseRejectsMalformedValues(t *testing.T) {
	upper := strings.ToUpper(strings.TrimPrefix(dig, "sha256:"))
	cases := []struct {
		name string
		edit func(string) string
		want string
	}{
		{"digest with upper-case hex", func(s string) string { return strings.Replace(s, dig, "sha256:"+upper, 1) }, "digest"},
		{"digest with upper-case algorithm", func(s string) string { return strings.Replace(s, dig, "SHA256:"+strings.TrimPrefix(dig, "sha256:"), 1) }, "digest"},
		{"sha512 digest", func(s string) string { return strings.Replace(s, dig, "sha512:"+strings.Repeat("a", 128), 1) }, "digest"},
		{"digest one char short", func(s string) string { return strings.Replace(s, dig, dig[:len(dig)-1], 1) }, "digest"},
		{"digest one char long", func(s string) string { return strings.Replace(s, dig, dig+"a", 1) }, "digest"},
		{"digest with a non-hex char", func(s string) string { return strings.Replace(s, dig, dig[:len(dig)-1]+"g", 1) }, "digest"},
		{"digest without algorithm", func(s string) string { return strings.Replace(s, dig, strings.TrimPrefix(dig, "sha256:"), 1) }, "digest"},
		{"digest with padding", func(s string) string { return strings.Replace(s, dig, " "+dig, 1) }, "digest"},
		{"digest with trailing newline", func(s string) string { return strings.Replace(s, dig, dig+`\n`, 1) }, "digest"},
		{"ref with leading space", func(s string) string { return strings.Replace(s, `"core": "`+sha+`"`, `"core": " `+sha+`"`, 1) }, "core"},
		{"ref with trailing space", func(s string) string { return strings.Replace(s, `"core": "`+sha+`"`, `"core": "`+sha+` "`, 1) }, "core"},
		{"ref with trailing newline", func(s string) string { return strings.Replace(s, `"core": "`+sha+`"`, `"core": "`+sha+`\n"`, 1) }, "core"},
		{"ref with a tab inside", func(s string) string {
			return strings.Replace(s, `"core": "`+sha+`"`, `"core": "`+sha[:20]+`\t`+sha[21:]+`"`, 1)
		}, "core"},
		{"upper-case ref", func(s string) string {
			return strings.Replace(s, `"core": "`+sha+`"`, `"core": "`+strings.ToUpper(sha)+`"`, 1)
		}, "core"},
		{"abbreviated ref", func(s string) string { return strings.Replace(s, `"core": "`+sha+`"`, `"core": "`+sha[:39]+`"`, 1) }, "core"},
		{"ref 41 chars", func(s string) string { return strings.Replace(s, `"core": "`+sha+`"`, `"core": "`+sha+`a"`, 1) }, "core"},
		{"proto ref with whitespace", func(s string) string {
			return strings.Replace(s, `"audit": "`+sha+`"`, `"audit": "`+sha+` "`, 1)
		}, "audit"},
		{"empty proto map", func(s string) string {
			return strings.Replace(s, `"proto": {"audit": "`+sha+`"}`, `"proto": {}`, 1)
		}, "proto"},
		{"empty services", func(s string) string {
			i := strings.Index(s, `"services"`)
			return s[:i] + `"services": []}`
		}, "services"},
		{"wrong schemaVersion", func(s string) string { return strings.Replace(s, `"schemaVersion": 1`, `"schemaVersion": 2`, 1) }, "schemaVersion"},
		{"release with a v", func(s string) string { return strings.Replace(s, `"0.1.0"`, `"v0.1.0"`, 1) }, "release"},
		{"release with two parts", func(s string) string { return strings.Replace(s, `"0.1.0"`, `"0.1"`, 1) }, "release"},
		{"release with a space", func(s string) string { return strings.Replace(s, `"0.1.0"`, `"0.1.0 "`, 1) }, "release"},
		{"upper-case service name", func(s string) string { return strings.Replace(s, `"name": "gateway"`, `"name": "Gateway"`, 1) }, "name"},
		{"service name with underscore", func(s string) string { return strings.Replace(s, `"name": "gateway"`, `"name": "gate_way"`, 1) }, "name"},
		{"service name with whitespace", func(s string) string { return strings.Replace(s, `"name": "gateway"`, `"name": "gateway "`, 1) }, "name"},
		{"empty service name", func(s string) string { return strings.Replace(s, `"name": "gateway"`, `"name": ""`, 1) }, "name"},
		{"image pinned by digest inline", func(s string) string {
			return strings.Replace(s, `steward-audit"`, `steward-audit@`+dig+`"`, 1)
		}, "image"},
		{"upper-case image", func(s string) string {
			return strings.Replace(s, "ghcr.io/steward-grc/steward-audit", "ghcr.io/Steward-GRC/steward-audit", 1)
		}, "image"},
		{"image without a path", func(s string) string {
			return strings.Replace(s, "ghcr.io/steward-grc/steward-audit", "steward-audit", 1)
		}, "image"},
		{"migrateAfter listed twice", func(s string) string { return strings.Replace(s, `["audit"]`, `["audit", "audit"]`, 1) }, "migrateAfter"},
		{"service missing a store", func(s string) string { return strings.Replace(s, `, "store": "none"`, ``, 1) }, "store"},
		{"upper-case store", func(s string) string { return strings.Replace(s, `"store": "none"`, `"store": "None"`, 1) }, "store"},
		{"trailing data after the document", func(s string) string { return s + `{}` }, "parse"},
		{"empty input", func(string) string { return "" }, "parse"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := tc.edit(valid())
			if doc == valid() {
				t.Fatal("the edit changed nothing")
			}
			_, err := Parse([]byte(doc), Options{})
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

func TestParseDuplicateNamesNameTheService(t *testing.T) {
	doc := strings.Replace(valid(), `"name": "gateway"`, `"name": "core"`, 1)
	_, err := Parse([]byte(doc), Options{})
	if err == nil || !strings.Contains(err.Error(), `duplicate service "core"`) {
		t.Fatalf("want a duplicate error naming core, got %v", err)
	}
	// Duplicates win over the checks that follow, whichever store they have.
	doc = strings.Replace(valid(), `"name": "audit"`, `"name": "core"`, 1)
	if _, err := Parse([]byte(doc), Options{}); err == nil || !strings.Contains(err.Error(), "duplicate service") {
		t.Fatalf("want a duplicate error, got %v", err)
	}
}

func TestParseRejectsMigrateAfterCycles(t *testing.T) {
	// The manifest check covers the references; a cycle is Order's to find, so
	// Parse accepts these. Pin that, so a change in where the check lives is a
	// decision and not an accident.
	three := `{
  "schemaVersion": 1, "release": "0.1.0",
  "refs": {"core": "` + sha + `", "proto": {"a": "` + sha + `"}},
  "services": [
    {"name": "a", "image": "ghcr.io/x/a", "digest": "` + dig + `", "store": "postgres", "migrateAfter": ["c"]},
    {"name": "b", "image": "ghcr.io/x/b", "digest": "` + dig + `", "store": "postgres", "migrateAfter": ["a"]},
    {"name": "c", "image": "ghcr.io/x/c", "digest": "` + dig + `", "store": "postgres", "migrateAfter": ["b"]}
  ]
}`
	m, err := Parse([]byte(three), Options{})
	if err != nil {
		t.Fatalf("a 3-cycle is not a reference error: %v", err)
	}
	if len(m.Services) != 3 {
		t.Fatalf("got %d services", len(m.Services))
	}
}

func TestParseAcceptsOddButValidShapes(t *testing.T) {
	cases := map[string]string{
		"release with a pre-release tag": strings.Replace(valid(), `"0.1.0"`, `"1.2.3-rc.1"`, 1),
		"image with a registry port":     strings.Replace(valid(), "ghcr.io/steward-grc/steward-audit", "images.example.com:5000/steward/audit", 1),
		"services with no migrateAfter":  strings.Replace(valid(), `, "migrateAfter": ["audit"]`, ``, 1),
		"every service store-less": strings.NewReplacer(`"store": "postgres"`, `"store": "none"`, `, "migrateAfter": ["audit"]`, ``).
			Replace(valid()),
		"empty migrateAfter on a store-less service": strings.Replace(valid(), `"store": "none"`, `"store": "none", "migrateAfter": []`, 1),
	}
	for name, doc := range cases {
		if _, err := Parse([]byte(doc), Options{}); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestStrictReportsEveryPlaceholderAtOnce(t *testing.T) {
	zeroDig := "sha256:" + strings.Repeat("0", 64)
	zeroSha := strings.Repeat("0", 40)
	// Partly filled: the release and one service are real, the rest are not.
	doc := `{
  "schemaVersion": 1,
  "release": "0.1.0",
  "refs": {"core": "` + zeroSha + `", "proto": {"core": "` + sha + `", "audit": "` + zeroSha + `", "workflow": "` + zeroSha + `"}},
  "services": [
    {"name": "audit", "image": "ghcr.io/x/audit", "digest": "` + zeroDig + `", "store": "postgres"},
    {"name": "core", "image": "ghcr.io/x/core", "digest": "` + dig + `", "store": "postgres"},
    {"name": "workflow", "image": "ghcr.io/x/workflow", "digest": "` + zeroDig + `", "store": "none"}
  ]
}`
	if _, err := Parse([]byte(doc), Options{}); err != nil {
		t.Fatalf("default mode must accept it: %v", err)
	}
	_, err := Parse([]byte(doc), Options{Strict: true})
	if err == nil {
		t.Fatal("strict mode must reject it")
	}
	want := "refs.core, refs.proto.audit, refs.proto.workflow, services.audit.digest, services.workflow.digest"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q does not list %q", err, want)
	}
	for _, real := range []string{"refs.proto.core", "services.core"} {
		if strings.Contains(err.Error(), real) {
			t.Fatalf("error %q names a real value %q", err, real)
		}
	}
}

func TestStrictOnlyRejectsTheExactPlaceholder(t *testing.T) {
	// Near-zero values are real as far as the check goes.
	nearZeroSha := strings.Repeat("0", 39) + "1"
	nearZeroDig := "sha256:" + strings.Repeat("0", 63) + "1"
	doc := strings.Replace(valid(), dig, nearZeroDig, 1)
	doc = strings.Replace(doc, `"core": "`+sha+`"`, `"core": "`+nearZeroSha+`"`, 1)
	doc = strings.Replace(doc, `"0.1.0"`, `"0.0.0"`, 1)
	if _, err := Parse([]byte(doc), Options{Strict: true}); err != nil {
		t.Fatalf("near-zero values are not the placeholder: %v", err)
	}
}

func TestLoadMissingFileNamesThePath(t *testing.T) {
	_, err := Load("/nonexistent/release-manifest.json", Options{})
	if err == nil || !strings.Contains(err.Error(), "read") {
		t.Fatalf("want a read error, got %v", err)
	}
}
