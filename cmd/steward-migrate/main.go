// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

// Command steward-migrate runs every service's migrations, in order, each
// against its own store. It plans by default and changes nothing without
// --apply.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"

	"github.com/Bugs5382/go-log"
	"github.com/spf13/cobra"

	"github.com/Steward-GRC/steward-release/internal/manifest"
	"github.com/Steward-GRC/steward-release/internal/migrate"
	"github.com/Steward-GRC/steward-release/internal/pgstore"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := newRoot().ExecuteContext(ctx); err != nil {
		os.Exit(1)
	}
}

func newRoot() *cobra.Command {
	var (
		manifestPath string
		root         string
		apply        bool
		strict       bool
	)
	logger := log.NewLoggerWithOptions("steward-migrate",
		log.WithOutput(os.Stderr), log.WithDefaultFormat(log.FormatConsole), log.WithDefaultLevel(log.LevelInfo))

	cmd := &cobra.Command{
		Use:   "steward-migrate",
		Short: "Run every service's migrations in order, dry-run unless --apply",
		Long: `Run every service's migrations, in the order the manifest gives, each against its own
store. By default it only reads each store and prints what would run. --apply runs it, and
only if every store was readable and clean.

Each store's DSN comes from STEWARD_MIGRATE_DSN_<SERVICE> (upper case, "-" as "_"). Each
service's migration files are read from <migrations-root>/<service>.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Applying from a template with placeholder digests is never what was meant.
			m, err := manifest.Load(manifestPath, manifest.Options{Strict: strict || apply})
			if err != nil {
				return report(cmd, logger, err)
			}
			logger.Info("manifest loaded", log.F("release", m.Release), log.F("services", len(m.Services)), log.F("apply", apply))
			rep, err := migrate.Run(cmd.Context(), &pgstore.Store{Root: root}, m.Services, migrate.Options{Apply: apply})
			if rep != nil {
				rep.Write(cmd.OutOrStdout())
			}
			if err != nil {
				return report(cmd, logger, err)
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&manifestPath, "manifest", "release-manifest.json", "the release manifest")
	f.StringVar(&root, "migrations-root", "migrations", "a directory holding one folder of migration files per service")
	f.BoolVar(&apply, "apply", false, "run the migrations (default: dry-run); implies --strict")
	f.BoolVar(&strict, "strict", false, "reject a manifest that still has placeholder values")
	return cmd
}

func report(cmd *cobra.Command, logger log.Logger, err error) error {
	logger.Error(err, "failed")
	if !errors.Is(err, migrate.ErrBlocked) {
		_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "error:", err)
	}
	return err
}
