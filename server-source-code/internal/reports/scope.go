package reports

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/PatchMon/PatchMon/server-source-code/internal/db"
)

// GroupRef is a host group frozen at collection time.
type GroupRef struct {
	ID   string
	Name string
}

// Scope is the resolved set of hosts a report may read.
type Scope struct {
	HostIDs      []string
	Groups       []GroupRef
	FleetWide    bool
	CustomerMode bool
}

// Scope errors; the worker records their text as the run's failure reason.
var (
	ErrScopeInvalid = errors.New("scope_invalid")
	ErrNoHosts      = errors.New("no_hosts")
	ErrTooManyHosts = errors.New("too_many_hosts")
)

// IsConfigError reports whether err is caused by the report's configuration
// (definition, groups, scope size) rather than by a transient failure. Config
// errors are deterministic: retrying the same slot cannot succeed.
func IsConfigError(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, ErrDefinition) || errors.Is(err, ErrScopeInvalid) || errors.Is(err, ErrNoHosts) || errors.Is(err, ErrTooManyHosts) || errors.Is(err, ErrRecipients)
}

// ValidateGroupIDs checks that every id names an existing host group and
// returns the groups sorted by name. Unknown ids are an ErrScopeInvalid that
// lists them, so the API can answer 400 and the worker can fail the run.
func ValidateGroupIDs(ctx context.Context, q *db.Queries, ids []string) ([]GroupRef, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := q.ForkReportGroupsByIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("load host groups: %w", err)
	}
	found := make(map[string]bool, len(rows))
	refs := make([]GroupRef, 0, len(rows))
	for _, r := range rows {
		found[r.ID] = true
		refs = append(refs, GroupRef{ID: r.ID, Name: r.Name})
	}
	var missing []string
	for _, id := range ids {
		if !found[id] {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("%w: unknown host group %s", ErrScopeInvalid, strings.Join(missing, ", "))
	}
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].Name != refs[j].Name {
			return refs[i].Name < refs[j].Name
		}
		return refs[i].ID < refs[j].ID
	})
	return refs, nil
}

// ResolveScope turns a definition into the host set the collector may read.
// It never falls back to the whole fleet: a customer report without groups,
// an unknown group, or a group selection without hosts is an error.
func ResolveScope(ctx context.Context, q *db.Queries, def Definition, customerMode bool) (Scope, error) {
	sc := Scope{CustomerMode: customerMode}
	if len(def.HostGroupIDs) == 0 {
		if customerMode {
			return Scope{}, fmt.Errorf("%w: a customer report needs at least one host group", ErrScopeInvalid)
		}
		ids, err := q.ForkReportAllHostIDs(ctx)
		if err != nil {
			return Scope{}, fmt.Errorf("load hosts: %w", err)
		}
		sc.FleetWide = true
		sc.HostIDs = ids
	} else {
		groups, err := ValidateGroupIDs(ctx, q, def.HostGroupIDs)
		if err != nil {
			return Scope{}, err
		}
		ids, err := q.GetHostIDsByGroupIDs(ctx, def.HostGroupIDs)
		if err != nil {
			return Scope{}, fmt.Errorf("load group hosts: %w", err)
		}
		sc.Groups = groups
		sc.HostIDs = ids
	}
	sort.Strings(sc.HostIDs)
	if len(sc.HostIDs) == 0 {
		return Scope{}, fmt.Errorf("%w: the selected host groups contain no hosts", ErrNoHosts)
	}
	if len(sc.HostIDs) > MaxHosts {
		return Scope{}, fmt.Errorf("%w: %d hosts in scope, at most %d are supported", ErrTooManyHosts, len(sc.HostIDs), MaxHosts)
	}
	return sc, nil
}
