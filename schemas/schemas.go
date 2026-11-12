// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

// Package schemas embeds the JSON Schemas this repo publishes.
package schemas

import _ "embed"

// ReleaseManifest is the JSON Schema of release-manifest.json.
//
//go:embed release-manifest.schema.json
var ReleaseManifest []byte
