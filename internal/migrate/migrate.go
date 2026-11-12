// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

// Package migrate plans and applies every service's migrations, in order,
// each against its own store.
package migrate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/Steward-GRC/steward-release/internal/manifest"
)

// ErrBlocked is returned when a plan cannot be applied: a store is dirty or
// could not be read. Nothing is applied in that case.
var ErrBlocked = errors.New("migrate: blocked")

// Status is what one store looks like next to its service's migrations.
type Status struct {
	Current uint   // the applied version; 0 on a store that has none
	Dirty   bool   // a previous run stopped half-way through Current
	Pending []uint // the versions that would be applied, ascending
}

// Store reads and migrates the database a service owns. Apply must run the
// same migration a service runs at its own start-up.
type Store interface {
	Status(ctx context.Context, service string) (Status, error)
	Apply(ctx context.Context, service string) error
}

// Options select what Run does.
type Options struct {
	// Apply migrates. The zero value only plans.
	Apply bool
}

// State of one step of a report.
type State string

// Step states.
const (
	StateUpToDate     State = "up-to-date"
	StatePending      State = "pending"
	StateDirty        State = "dirty"
	StateError        State = "error"
	StateApplied      State = "applied"
	StateFailed       State = "failed"
	StateNotAttempted State = "not-attempted"
)

// Step is one service in a report.
type Step struct {
	Service string
	State   State
	Status  Status
	Err     error
}

// Report is what Run found and did.
type Report struct {
	Steps   []Step
	Applied bool // true when Run was asked to apply
}

// Order returns the services that own a store, dependencies first. At each
// step the earliest-listed service whose dependencies are done goes next, so
// the result is the same on every run.
func Order(services []manifest.Service) ([]manifest.Service, error) {
	var stores []manifest.Service
	index := map[string]int{}
	for _, s := range services {
		if s.Store == manifest.StorePostgres {
			index[s.Name] = len(stores)
			stores = append(stores, s)
		}
	}
	for _, s := range stores {
		for _, dep := range s.MigrateAfter {
			if _, ok := index[dep]; !ok {
				return nil, fmt.Errorf("migrate: %q runs after unknown service %q", s.Name, dep)
			}
		}
	}
	done := make(map[string]bool, len(stores))
	out := make([]manifest.Service, 0, len(stores))
	for len(out) < len(stores) {
		progressed := false
		for _, s := range stores {
			if done[s.Name] || !allDone(s.MigrateAfter, done) {
				continue
			}
			done[s.Name] = true
			out = append(out, s)
			progressed = true
			break
		}
		if !progressed {
			var stuck []string
			for _, s := range stores {
				if !done[s.Name] {
					stuck = append(stuck, s.Name)
				}
			}
			return nil, fmt.Errorf("migrate: cycle in migrateAfter among %s", strings.Join(stuck, ", "))
		}
	}
	return out, nil
}

func allDone(deps []string, done map[string]bool) bool {
	for _, d := range deps {
		if !done[d] {
			return false
		}
	}
	return true
}

// Run plans every store in order, then, only when asked, applies. The plan is
// always built first for every store, and a dirty or unreadable one blocks
// the apply before anything has run. Apply stops at the first failure and
// leaves the rest untouched; running it again continues from there.
func Run(ctx context.Context, store Store, services []manifest.Service, opts Options) (*Report, error) {
	ordered, err := Order(services)
	if err != nil {
		return nil, err
	}
	rep := &Report{Applied: opts.Apply}
	blocked := false
	for _, s := range ordered {
		st, err := store.Status(ctx, s.Name)
		step := Step{Service: s.Name, Status: st}
		switch {
		case err != nil:
			step.State, step.Err, blocked = StateError, err, true
		case st.Dirty:
			step.State, blocked = StateDirty, true
		case len(st.Pending) > 0:
			step.State = StatePending
		default:
			step.State = StateUpToDate
		}
		rep.Steps = append(rep.Steps, step)
	}
	if blocked {
		return rep, ErrBlocked
	}
	if !opts.Apply {
		return rep, nil
	}
	for i := range rep.Steps {
		step := &rep.Steps[i]
		if step.State != StatePending {
			continue
		}
		if err := store.Apply(ctx, step.Service); err != nil {
			step.State, step.Err = StateFailed, err
			for j := i + 1; j < len(rep.Steps); j++ {
				if rep.Steps[j].State == StatePending {
					rep.Steps[j].State = StateNotAttempted
				}
			}
			return rep, fmt.Errorf("migrate: %s: %w", step.Service, err)
		}
		step.State = StateApplied
	}
	return rep, nil
}

// Write prints the report.
func (r *Report) Write(w io.Writer) {
	mode := "dry-run"
	if r.Applied {
		mode = "apply"
	}
	_, _ = fmt.Fprintf(w, "steward-migrate %s, in order:\n", mode)
	changes := false
	for i, s := range r.Steps {
		line := fmt.Sprintf("%2d. %-14s at %-4d %s", i+1, s.Service, s.Status.Current, s.State)
		switch {
		case s.Err != nil:
			line += ": " + s.Err.Error()
		case s.State == StateDirty:
			line += ": a previous migration stopped half-way; fix it by hand before migrating"
		case len(s.Status.Pending) > 0:
			line += ": " + versionsText(s.Status.Pending)
			changes = s.State == StatePending
		}
		_, _ = fmt.Fprintln(w, line)
	}
	if !r.Applied {
		if changes {
			_, _ = fmt.Fprintln(w, "nothing was changed; run again with --apply to migrate")
		} else {
			_, _ = fmt.Fprintln(w, "nothing was changed; nothing is pending")
		}
	}
}

func versionsText(v []uint) string {
	parts := make([]string, len(v))
	for i, n := range v {
		parts[i] = strconv.FormatUint(uint64(n), 10)
	}
	return "versions " + strings.Join(parts, ", ")
}

var upFile = regexp.MustCompile(`^(\d+)_.*\.up\.sql$`)

// PendingVersions lists the migration versions in dir that are newer than
// current, ascending.
func PendingVersions(dir string, current uint) ([]uint, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("migrate: read migrations: %w", err)
	}
	var out []uint
	for _, e := range entries {
		m := upFile.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		v, err := strconv.ParseUint(m[1], 10, 32)
		if err != nil {
			return nil, fmt.Errorf("migrate: version in %q: %w", e.Name(), err)
		}
		if uint(v) > current {
			out = append(out, uint(v))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}
