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

func svc(name string, after ...string) manifest.Service {
	return manifest.Service{Name: name, Store: manifest.StorePostgres, MigrateAfter: after}
}

func names(ss []manifest.Service) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = s.Name
	}
	return out
}

func TestOrderKeepsManifestOrderWithoutEdges(t *testing.T) {
	got, err := Order([]manifest.Service{svc("b"), svc("a"), svc("c")})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"b", "a", "c"}; !reflect.DeepEqual(names(got), want) {
		t.Fatalf("got %v want %v", names(got), want)
	}
}

func TestOrderPutsDependenciesFirstAndBreaksTiesByManifestOrder(t *testing.T) {
	in := []manifest.Service{
		svc("workflow", "core", "audit"),
		svc("core", "audit"),
		svc("reporting", "audit"),
		svc("audit"),
	}
	got, err := Order(in)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"audit", "core", "workflow", "reporting"}; !reflect.DeepEqual(names(got), want) {
		t.Fatalf("got %v want %v", names(got), want)
	}
}

func TestOrderSkipsServicesWithoutAStore(t *testing.T) {
	got, err := Order([]manifest.Service{svc("a"), {Name: "gateway", Store: manifest.StoreNone}, svc("b")})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"a", "b"}; !reflect.DeepEqual(names(got), want) {
		t.Fatalf("got %v want %v", names(got), want)
	}
}

func TestOrderRejectsCycleAndUnknown(t *testing.T) {
	if _, err := Order([]manifest.Service{svc("a", "b"), svc("b", "a"), svc("c")}); err == nil || !strings.Contains(err.Error(), "cycle") || !strings.Contains(err.Error(), "a") {
		t.Fatalf("want a cycle error naming the members, got %v", err)
	}
	if _, err := Order([]manifest.Service{svc("a", "nope")}); err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("want an unknown-service error, got %v", err)
	}
}

// fakeStore stands in for the databases: it holds a status per service and
// records every Apply, in order.
type fakeStore struct {
	status  map[string]Status
	failOn  string
	statErr map[string]error
	applied []string
}

func (f *fakeStore) Status(_ context.Context, s string) (Status, error) {
	if err := f.statErr[s]; err != nil {
		return Status{}, err
	}
	return f.status[s], nil
}

func (f *fakeStore) Apply(_ context.Context, s string) error {
	if s == f.failOn {
		return errors.New("boom")
	}
	f.applied = append(f.applied, s)
	st := f.status[s]
	st.Pending = nil
	f.status[s] = st
	return nil
}

func fixture() (*fakeStore, []manifest.Service) {
	return &fakeStore{
		status: map[string]Status{
			"audit": {Current: 3},
			"core":  {Current: 1, Pending: []uint{2, 3}},
			"ai":    {Pending: []uint{1}},
		},
		statErr: map[string]error{},
	}, []manifest.Service{svc("core", "audit"), svc("ai"), svc("audit"), {Name: "gateway", Store: manifest.StoreNone}}
}

func TestDryRunReportsThePlanAndAppliesNothing(t *testing.T) {
	st, svcs := fixture()
	rep, err := Run(context.Background(), st, svcs, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(st.applied) != 0 {
		t.Fatalf("dry-run applied %v", st.applied)
	}
	if rep.Applied {
		t.Fatal("report claims it applied")
	}
	got := []string{}
	for _, s := range rep.Steps {
		got = append(got, s.Service+":"+string(s.State))
	}
	want := []string{"ai:pending", "audit:up-to-date", "core:pending"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestApplyRunsInOrderAndSkipsUpToDate(t *testing.T) {
	st, svcs := fixture()
	rep, err := Run(context.Background(), st, svcs, Options{Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"ai", "core"}; !reflect.DeepEqual(st.applied, want) {
		t.Fatalf("applied %v want %v", st.applied, want)
	}
	if !rep.Applied || rep.Steps[2].State != StateApplied || rep.Steps[1].State != StateUpToDate {
		t.Fatalf("unexpected report: %+v", rep)
	}
}

func TestApplyStopsAtTheFirstFailure(t *testing.T) {
	st, svcs := fixture()
	st.failOn = "ai"
	rep, err := Run(context.Background(), st, svcs, Options{Apply: true})
	if err == nil || !strings.Contains(err.Error(), "ai") {
		t.Fatalf("want an error naming ai, got %v", err)
	}
	if len(st.applied) != 0 {
		t.Fatalf("applied %v after ai failed", st.applied)
	}
	if rep.Steps[0].State != StateFailed || rep.Steps[2].State != StateNotAttempted {
		t.Fatalf("unexpected states: %+v", rep.Steps)
	}
}

func TestDirtyStoreBlocksApplyButIsReportedInDryRun(t *testing.T) {
	st, svcs := fixture()
	st.status["core"] = Status{Current: 1, Dirty: true, Pending: []uint{2}}
	rep, err := Run(context.Background(), st, svcs, Options{})
	if !errors.Is(err, ErrBlocked) || rep.Steps[2].State != StateDirty {
		t.Fatalf("dry-run: want ErrBlocked and a dirty step, got %v %+v", err, rep.Steps)
	}
	_, err = Run(context.Background(), st, svcs, Options{Apply: true})
	if !errors.Is(err, ErrBlocked) {
		t.Fatalf("apply: want ErrBlocked, got %v", err)
	}
	if len(st.applied) != 0 {
		t.Fatalf("a blocked apply still applied %v", st.applied)
	}
}

func TestUnreadableStoreBlocksApplyBeforeAnythingRuns(t *testing.T) {
	st, svcs := fixture()
	st.statErr["ai"] = errors.New("no DSN")
	_, err := Run(context.Background(), st, svcs, Options{Apply: true})
	if !errors.Is(err, ErrBlocked) || len(st.applied) != 0 {
		t.Fatalf("want ErrBlocked with nothing applied, got %v and %v", err, st.applied)
	}
}

func TestPendingVersions(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"0001_baseline.up.sql", "0001_baseline.down.sql", "0002_a.up.sql", "0003_b.up.sql", "notes.md"} {
		if err := os.WriteFile(filepath.Join(dir, f), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	got, err := PendingVersions(dir, 1)
	if err != nil {
		t.Fatal(err)
	}
	if want := []uint{2, 3}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	if got, _ := PendingVersions(dir, 3); len(got) != 0 {
		t.Fatalf("got %v want none", got)
	}
	if _, err := PendingVersions(filepath.Join(dir, "missing"), 0); err == nil {
		t.Fatal("a missing directory must be an error")
	}
}

func TestWriteReport(t *testing.T) {
	st, svcs := fixture()
	rep, _ := Run(context.Background(), st, svcs, Options{})
	var b strings.Builder
	rep.Write(&b)
	out := b.String()
	for _, want := range []string{"dry-run", "core", "versions 2, 3", "nothing was changed", "--apply"} {
		if !strings.Contains(out, want) {
			t.Fatalf("report lacks %q:\n%s", want, out)
		}
	}
}
