// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

// Package manifest loads and validates the pinned release manifest.
package manifest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/Steward-GRC/steward-release/schemas"
)

// Store values of a service.
const (
	StorePostgres = "postgres"
	StoreNone     = "none"
)

const (
	placeholderRelease = "0.0.0-placeholder"
	zeroDigest         = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	zeroCommit         = "0000000000000000000000000000000000000000"
)

// Manifest is the release manifest.
type Manifest struct {
	SchemaVersion int       `json:"schemaVersion"`
	Release       string    `json:"release"`
	Refs          Refs      `json:"refs"`
	Services      []Service `json:"services"`
}

// Refs are the commits the release was built against.
type Refs struct {
	Core  string            `json:"core"`
	Proto map[string]string `json:"proto"`
}

// Service is one image of the release.
type Service struct {
	Name         string   `json:"name"`
	Image        string   `json:"image"`
	Digest       string   `json:"digest"`
	Store        string   `json:"store"`
	MigrateAfter []string `json:"migrateAfter,omitempty"`
}

// Options tune validation.
type Options struct {
	// Strict rejects the all-zero placeholders of the committed template, so a
	// manifest that is cut into a release is a real one.
	Strict bool
}

// Load reads and validates the manifest at path.
func Load(path string, opts Options) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("manifest: read: %w", err)
	}
	return Parse(data, opts)
}

// Parse validates data against the schema, then against the rules a schema
// cannot state (unique names, references between services, placeholders).
func Parse(data []byte, opts Options) (*Manifest, error) {
	var doc any
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("manifest: parse: %w", err)
	}
	if err := validateSchema(doc); err != nil {
		return nil, fmt.Errorf("manifest: schema: %w", err)
	}
	var m Manifest
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("manifest: parse: %w", err)
	}
	if err := m.check(); err != nil {
		return nil, fmt.Errorf("manifest: %w", err)
	}
	if opts.Strict {
		if err := m.rejectPlaceholders(); err != nil {
			return nil, fmt.Errorf("manifest: %w", err)
		}
	}
	return &m, nil
}

func validateSchema(doc any) error {
	schemaDoc, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemas.ReleaseManifest))
	if err != nil {
		return err
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource("release-manifest.schema.json", schemaDoc); err != nil {
		return err
	}
	s, err := c.Compile("release-manifest.schema.json")
	if err != nil {
		return err
	}
	return s.Validate(doc)
}

func (m *Manifest) check() error {
	byName := make(map[string]Service, len(m.Services))
	for _, s := range m.Services {
		if _, dup := byName[s.Name]; dup {
			return fmt.Errorf("duplicate service %q", s.Name)
		}
		byName[s.Name] = s
	}
	for name := range m.Refs.Proto {
		if _, ok := byName[name]; !ok {
			return fmt.Errorf("proto ref for unknown service %q", name)
		}
	}
	for _, s := range m.Services {
		if len(s.MigrateAfter) > 0 && s.Store != StorePostgres {
			return fmt.Errorf("service %q has no store, so migrateAfter does not apply", s.Name)
		}
		for _, dep := range s.MigrateAfter {
			d, ok := byName[dep]
			switch {
			case dep == s.Name:
				return fmt.Errorf("service %q lists itself in migrateAfter", s.Name)
			case !ok:
				return fmt.Errorf("service %q: migrateAfter names unknown service %q", s.Name, dep)
			case d.Store != StorePostgres:
				return fmt.Errorf("service %q: migrateAfter names %q, which has no store", s.Name, dep)
			}
		}
	}
	return nil
}

func (m *Manifest) rejectPlaceholders() error {
	var found []string
	if m.Release == placeholderRelease {
		found = append(found, "release")
	}
	if m.Refs.Core == zeroCommit {
		found = append(found, "refs.core")
	}
	protoNames := make([]string, 0, len(m.Refs.Proto))
	for name := range m.Refs.Proto {
		protoNames = append(protoNames, name)
	}
	sort.Strings(protoNames)
	for _, name := range protoNames {
		if m.Refs.Proto[name] == zeroCommit {
			found = append(found, "refs.proto."+name)
		}
	}
	for _, s := range m.Services {
		if s.Digest == zeroDigest {
			found = append(found, "services."+s.Name+".digest")
		}
	}
	if len(found) > 0 {
		return fmt.Errorf("placeholder values remain (a release needs real ones): %s", strings.Join(found, ", "))
	}
	return nil
}
