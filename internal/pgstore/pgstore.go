// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

// Package pgstore is the Postgres Store: it reads a service's version with
// golang-migrate and applies with the same call every service makes at
// start-up.
package pgstore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	postgres "github.com/Bugs5382/go-postgres"
	gm "github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres" // registers the postgres driver
	_ "github.com/golang-migrate/migrate/v4/source/file"       // registers file://

	"github.com/Steward-GRC/steward-release/internal/migrate"
)

// Store migrates the databases of the services, reading each one's migrations
// from <Root>/<service> and its DSN from the environment.
type Store struct {
	Root   string
	Getenv func(string) string // os.Getenv when nil
}

var _ migrate.Store = (*Store)(nil)

// EnvName is the variable that holds a service's DSN. DSNs come only from the
// environment: they are secrets, and a flag shows up in process listings.
func EnvName(service string) string {
	return "STEWARD_MIGRATE_DSN_" + strings.ToUpper(strings.ReplaceAll(service, "-", "_"))
}

func (s *Store) target(service string) (dsn, dir string, err error) {
	get := s.Getenv
	if get == nil {
		get = os.Getenv
	}
	dsn = get(EnvName(service))
	if dsn == "" {
		return "", "", fmt.Errorf("%s is not set", EnvName(service))
	}
	return dsn, filepath.Join(s.Root, service), nil
}

// Status reads the applied version and lists the files still to apply. It
// writes nothing. Errors never include the DSN.
func (s *Store) Status(_ context.Context, service string) (migrate.Status, error) {
	dsn, dir, err := s.target(service)
	if err != nil {
		return migrate.Status{}, err
	}
	m, err := gm.New("file://"+dir, dsn)
	if err != nil {
		return migrate.Status{}, fmt.Errorf("open %s: %s", service, redact(err, dsn))
	}
	defer func() { _, _ = m.Close() }()
	v, dirty, err := m.Version()
	if err != nil && !errors.Is(err, gm.ErrNilVersion) {
		return migrate.Status{}, fmt.Errorf("read version of %s: %s", service, redact(err, dsn))
	}
	pending, err := migrate.PendingVersions(dir, v)
	if err != nil {
		return migrate.Status{}, err
	}
	return migrate.Status{Current: v, Dirty: dirty, Pending: pending}, nil
}

// Apply migrates the service's store to the newest version.
func (s *Store) Apply(_ context.Context, service string) error {
	dsn, dir, err := s.target(service)
	if err != nil {
		return err
	}
	if err := postgres.Migrate(dsn, dir); err != nil {
		return errors.New(redact(err, dsn))
	}
	return nil
}

func redact(err error, dsn string) string {
	return strings.ReplaceAll(err.Error(), dsn, "<dsn>")
}
