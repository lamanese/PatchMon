package reports

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestResolveScopeGroups(t *testing.T) {
	d := newReportsTestDB(t)
	ctx := context.Background()
	gA := insertGroup(t, d, "Group A")
	gB := insertGroup(t, d, "Group B")
	gEmpty := insertGroup(t, d, "Empty")
	a1 := insertHost(t, d, "a1", gA)
	a2 := insertHost(t, d, "a2", gA)
	b1 := insertHost(t, d, "b1", gB)
	shared := insertHost(t, d, "shared", gA, gB)
	_ = insertHost(t, d, "ungrouped")

	t.Run("single group", func(t *testing.T) {
		def := Definition{HostGroupIDs: []string{gA}}
		sc, err := ResolveScope(ctx, d.Queries, def, false)
		if err != nil {
			t.Fatal(err)
		}
		if sc.FleetWide || sc.CustomerMode {
			t.Fatalf("flags %+v", sc)
		}
		if len(sc.Groups) != 1 || sc.Groups[0].Name != "Group A" || sc.Groups[0].ID != gA {
			t.Fatalf("groups %+v", sc.Groups)
		}
		want := map[string]bool{a1: true, a2: true, shared: true}
		if len(sc.HostIDs) != 3 {
			t.Fatalf("host ids %v", sc.HostIDs)
		}
		for _, id := range sc.HostIDs {
			if !want[id] {
				t.Fatalf("unexpected host %s", id)
			}
		}
	})

	t.Run("two groups sharing a host list it once", func(t *testing.T) {
		sc, err := ResolveScope(ctx, d.Queries, Definition{HostGroupIDs: []string{gB, gA}}, true)
		if err != nil {
			t.Fatal(err)
		}
		if len(sc.HostIDs) != 4 || !sc.CustomerMode {
			t.Fatalf("%+v", sc)
		}
		if sc.Groups[0].Name != "Group A" || sc.Groups[1].Name != "Group B" {
			t.Fatalf("groups must be sorted by name: %+v", sc.Groups)
		}
		seen := map[string]int{}
		for _, id := range sc.HostIDs {
			seen[id]++
		}
		if seen[shared] != 1 || seen[b1] != 1 {
			t.Fatalf("duplicates: %v", seen)
		}
	})

	t.Run("unknown group is scope_invalid and names the id", func(t *testing.T) {
		_, err := ResolveScope(ctx, d.Queries, Definition{HostGroupIDs: []string{gA, "does-not-exist"}}, false)
		if !errors.Is(err, ErrScopeInvalid) || !strings.Contains(err.Error(), "does-not-exist") {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("empty group is no_hosts", func(t *testing.T) {
		_, err := ResolveScope(ctx, d.Queries, Definition{HostGroupIDs: []string{gEmpty}}, false)
		if !errors.Is(err, ErrNoHosts) {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("customer mode without groups is scope_invalid", func(t *testing.T) {
		_, err := ResolveScope(ctx, d.Queries, Definition{}, true)
		if !errors.Is(err, ErrScopeInvalid) {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("internal report without groups is fleet-wide", func(t *testing.T) {
		sc, err := ResolveScope(ctx, d.Queries, Definition{}, false)
		if err != nil {
			t.Fatal(err)
		}
		if !sc.FleetWide || len(sc.HostIDs) != 5 || len(sc.Groups) != 0 {
			t.Fatalf("%+v", sc)
		}
	})
}

func TestValidateGroupIDs(t *testing.T) {
	d := newReportsTestDB(t)
	ctx := context.Background()
	g := insertGroup(t, d, "Only")
	refs, err := ValidateGroupIDs(ctx, d.Queries, []string{g})
	if err != nil || len(refs) != 1 || refs[0].Name != "Only" {
		t.Fatalf("%v %+v", err, refs)
	}
	if refs, err := ValidateGroupIDs(ctx, d.Queries, nil); err != nil || len(refs) != 0 {
		t.Fatalf("nil ids: %v %+v", err, refs)
	}
	if _, err := ValidateGroupIDs(ctx, d.Queries, []string{g, "ghost"}); !errors.Is(err, ErrScopeInvalid) {
		t.Fatalf("got %v", err)
	}
}
