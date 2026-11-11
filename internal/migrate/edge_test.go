// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package migrate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Steward-GRC/steward-release/internal/manifest"
)

func TestOrderEdgeCases(t *testing.T) {
	cases := []struct {
		name string
		in   []manifest.Service
		want []string
	}{
		{"empty", nil, []string{}},
		{"only store-less services", []manifest.Service{{Name: "g", Store: manifest.StoreNone}}, []string{}},
		{"dependency listed after its dependant", []manifest.Service{svc("c", "b"), svc("b", "a"), svc("a")}, []string{"a", "b", "c"}},
		{"diamond", []manifest.Service{svc("d", "b", "c"), svc("c", "a"), svc("b", "a"), svc("a")}, []string{"a", "c", "b", "d"}},
		{"independent services keep their place around a chain", []manifest.Service{svc("x"), svc("b", "a"), svc("y"), svc("a")}, []string{"x", "y", "a", "b"}},
		{"a free service listed first goes first even when a chain follows", []manifest.Service{svc("z"), svc("b", "a"), svc("a")}, []string{"z", "a", "b"}},
		{"a unlocked dependant waits for the earlier-listed free service", []manifest.Service{svc("b", "a"), svc("a"), svc("c")}, []string{"a", "b", "c"}},
		{"a store-less service between stores", []manifest.Service{svc("b", "a"), {Name: "g", Store: manifest.StoreNone}, svc("a")}, []string{"a", "b"}},
		{"empty migrateAfter", []manifest.Service{svc("a"), {Name: "b", Store: manifest.StorePostgres, MigrateAfter: []string{}}}, []string{"a", "b"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Order(tc.in)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(names(got), tc.want) {
				t.Fatalf("got %v want %v", names(got), tc.want)
			}
			// The same input gives the same order every time.
			again, _ := Order(tc.in)
			if !reflect.DeepEqual(names(again), names(got)) {
				t.Fatalf("not stable: %v then %v", names(got), names(again))
			}
		})
	}
}

func TestOrderDoesNotMutateItsInput(t *testing.T) {
	in := []manifest.Service{svc("b", "a"), svc("a")}
	if _, err := Order(in); err != nil {
		t.Fatal(err)
	}
	if in[0].Name != "b" || in[1].Name != "a" {
		t.Fatalf("input reordered: %v", names(in))
	}
}

func TestOrderCycles(t *testing.T) {
	cases := []struct {
		name    string
		in      []manifest.Service
		members []string
		absent  []string
	}{
		{"self reference", []manifest.Service{svc("a", "a"), svc("b")}, []string{"a"}, []string{"b"}},
		{"three members", []manifest.Service{svc("a", "c"), svc("b", "a"), svc("c", "b"), svc("free")}, []string{"a", "b", "c"}, []string{"free"}},
		{"cycle behind a valid prefix", []manifest.Service{svc("root"), svc("a", "root", "b"), svc("b", "a")}, []string{"a", "b"}, []string{"root"}},
		{"dependant of a cycle is stuck too", []manifest.Service{svc("a", "b"), svc("b", "a"), svc("tail", "a")}, []string{"a", "b", "tail"}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Order(tc.in)
			if err == nil || !strings.Contains(err.Error(), "cycle") {
				t.Fatalf("want a cycle error, got %v", err)
			}
			list := strings.TrimPrefix(err.Error()[strings.Index(err.Error(), "among "):], "among ")
			got := strings.Split(list, ", ")
			for _, m := range tc.members {
				found := false
				for _, g := range got {
					found = found || g == m
				}
				if !found {
					t.Errorf("%q should be named in %q", m, err)
				}
			}
			for _, a := range tc.absent {
				for _, g := range got {
					if g == a {
						t.Errorf("%q should not be named in %q", a, err)
					}
				}
			}
		})
	}
}

func TestOrderRejectsAStoreLessDependency(t *testing.T) {
	_, err := Order([]manifest.Service{svc("a", "g"), {Name: "g", Store: manifest.StoreNone}})
	if err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("want an error for a dependency without a store, got %v", err)
	}
}

func TestOrderDuplicateNamesFailInsteadOfHanging(t *testing.T) {
	// The manifest rejects duplicates; Order must still stop if handed some.
	_, err := Order([]manifest.Service{svc("a"), svc("a")})
	if err == nil {
		t.Fatal("duplicate names must not produce an order")
	}
}

func TestRunWithNoStores(t *testing.T) {
	st := &fakeStore{status: map[string]Status{}, statErr: map[string]error{}}
	for _, apply := range []bool{false, true} {
		rep, err := Run(context.Background(), st, nil, Options{Apply: apply})
		if err != nil {
			t.Fatalf("apply=%v: %v", apply, err)
		}
		if len(rep.Steps) != 0 || len(st.applied) != 0 {
			t.Fatalf("apply=%v: steps %v applied %v", apply, rep.Steps, st.applied)
		}
		var b strings.Builder
		rep.Write(&b)
		if !strings.Contains(b.String(), "steward-migrate") {
			t.Fatalf("report has no header:\n%s", b.String())
		}
	}
	rep, _ := Run(context.Background(), st, nil, Options{})
	var b strings.Builder
	rep.Write(&b)
	if !strings.Contains(b.String(), "nothing is pending") {
		t.Fatalf("an empty plan should say nothing is pending:\n%s", b.String())
	}
}

func TestRunReturnsOrderErrorsWithoutTouchingAStore(t *testing.T) {
	st, _ := fixture()
	rep, err := Run(context.Background(), st, []manifest.Service{svc("a", "b"), svc("b", "a")}, Options{Apply: true})
	if err == nil || rep != nil {
		t.Fatalf("want an error and no report, got %v %v", err, rep)
	}
	if len(st.applied) != 0 {
		t.Fatalf("applied %v", st.applied)
	}
}

func TestRunStoresThatShareAServerStayIndependent(t *testing.T) {
	// Two services whose stores live in the same database server are still two
	// stores: each is read and applied on its own, in manifest order.
	st := &fakeStore{
		status:  map[string]Status{"a": {Pending: []uint{1}}, "b": {Pending: []uint{1, 2}}},
		statErr: map[string]error{},
	}
	rep, err := Run(context.Background(), st, []manifest.Service{svc("b"), svc("a")}, Options{Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"b", "a"}; !reflect.DeepEqual(st.applied, want) {
		t.Fatalf("applied %v want %v", st.applied, want)
	}
	if rep.Steps[0].State != StateApplied || rep.Steps[1].State != StateApplied {
		t.Fatalf("states: %+v", rep.Steps)
	}
}

func TestApplyFailureMarksOnlyPendingStepsNotAttempted(t *testing.T) {
	st := &fakeStore{
		status: map[string]Status{
			"a": {Pending: []uint{1}}, "b": {Current: 4}, "c": {Pending: []uint{1}}, "d": {Pending: []uint{1}},
		},
		statErr: map[string]error{},
		failOn:  "c",
	}
	rep, err := Run(context.Background(), st, []manifest.Service{svc("a"), svc("b"), svc("c"), svc("d")}, Options{Apply: true})
	if err == nil || !strings.Contains(err.Error(), "c") {
		t.Fatalf("want an error naming c, got %v", err)
	}
	got := []State{}
	for _, s := range rep.Steps {
		got = append(got, s.State)
	}
	want := []State{StateApplied, StateUpToDate, StateFailed, StateNotAttempted}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	if rep.Steps[2].Err == nil {
		t.Fatal("the failed step should carry its error")
	}
	// Running again continues from the failure.
	st.failOn = ""
	rep, err = Run(context.Background(), st, []manifest.Service{svc("a"), svc("b"), svc("c"), svc("d")}, Options{Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"a", "c", "d"}; !reflect.DeepEqual(st.applied, want) {
		t.Fatalf("applied %v want %v", st.applied, want)
	}
	if rep.Steps[0].State != StateUpToDate {
		t.Fatalf("a was applied before and should now be up to date: %+v", rep.Steps[0])
	}
}

func TestEveryStoreIsReadBeforeAnyIsBlocked(t *testing.T) {
	// A dirty first store must not hide a problem in a later one.
	st, svcs := fixture()
	st.status["ai"] = Status{Dirty: true, Pending: []uint{1}}
	st.statErr["core"] = errors.New("unreachable")
	rep, err := Run(context.Background(), st, svcs, Options{Apply: true})
	if !errors.Is(err, ErrBlocked) {
		t.Fatalf("want ErrBlocked, got %v", err)
	}
	if len(rep.Steps) != 3 || rep.Steps[0].State != StateDirty || rep.Steps[2].State != StateError {
		t.Fatalf("both problems should be reported: %+v", rep.Steps)
	}
	if len(st.applied) != 0 {
		t.Fatalf("applied %v", st.applied)
	}
}

func TestWriteReportStates(t *testing.T) {
	rep := &Report{Steps: []Step{
		{Service: "a", State: StateUpToDate, Status: Status{Current: 7}},
		{Service: "b", State: StateDirty, Status: Status{Current: 2, Dirty: true}},
		{Service: "c", State: StateError, Err: errors.New("no DSN")},
		{Service: "d", State: StatePending, Status: Status{Pending: []uint{1}}},
	}}
	var b strings.Builder
	rep.Write(&b)
	out := b.String()
	for _, want := range []string{"dry-run", "up-to-date", "fix it by hand", "no DSN", "versions 1"} {
		if !strings.Contains(out, want) {
			t.Fatalf("report lacks %q:\n%s", want, out)
		}
	}
	rep.Applied = true
	b.Reset()
	rep.Write(&b)
	if !strings.Contains(b.String(), "steward-migrate apply") || strings.Contains(b.String(), "--apply") {
		t.Fatalf("an applied report is not a dry-run:\n%s", b.String())
	}
}

func TestWriteDryRunSaysChangesArePendingWhenAnyStepIs(t *testing.T) {
	// A pending step followed by a step with nothing to do, or by a blocked
	// one, must not turn the closing line into "nothing is pending".
	rep := &Report{Steps: []Step{
		{Service: "a", State: StatePending, Status: Status{Pending: []uint{1}}},
		{Service: "b", State: StateDirty, Status: Status{Current: 1, Dirty: true, Pending: []uint{2}}},
	}}
	var b strings.Builder
	rep.Write(&b)
	if strings.Contains(b.String(), "nothing is pending") {
		t.Fatalf("a is pending, yet the report says nothing is:\n%s", b.String())
	}
}

func TestPendingVersionsEdgeCases(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{
		"000010_ten.up.sql", "000002_two.up.sql", "9_nine.up.sql", // numeric, not lexical, order
		"0001_baseline.down.sql", "0003_no_direction.sql", "README.up.sql", "x_0004.up.sql", "0005.up.sql", // not migrations
		"0006_UPPER.UP.SQL",
	} {
		if err := os.WriteFile(filepath.Join(dir, f), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "0007_dir.up.sql"), 0o700); err != nil {
		t.Fatal(err)
	}
	got, err := PendingVersions(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	// A directory named like a migration still counts as a name match; the
	// loader, not this listing, rejects it.
	if want := []uint{2, 7, 9, 10}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	if got, _ := PendingVersions(dir, 10); len(got) != 0 {
		t.Fatalf("got %v want none", got)
	}
}

func TestPendingVersionsEmptyDirAndHugeVersion(t *testing.T) {
	dir := t.TempDir()
	got, err := PendingVersions(dir, 0)
	if err != nil || len(got) != 0 {
		t.Fatalf("an empty directory has nothing pending: %v %v", got, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "99999999999_big.up.sql"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := PendingVersions(dir, 0); err == nil {
		t.Fatal("a version that overflows 32 bits must be an error, not a wrapped number")
	}
}
