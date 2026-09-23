package reports

import (
	"strings"
	"testing"
)

func TestParseDefinitionDefaults(t *testing.T) {
	for _, raw := range []string{"", "null", "{}"} {
		def, err := ParseDefinition([]byte(raw))
		if err != nil {
			t.Fatalf("%q: unexpected error %v", raw, err)
		}
		if def.Version != 2 || def.Language != "en" || def.PeriodDays != 30 || def.Limits.TopHosts != DefaultTopHosts {
			t.Fatalf("%q: bad defaults %+v", raw, def)
		}
		if strings.Join(def.Sections, ",") != strings.Join(DefaultSections, ",") {
			t.Fatalf("%q: sections %v", raw, def.Sections)
		}
		if len(def.HostGroupIDs) != 0 {
			t.Fatalf("%q: groups %v", raw, def.HostGroupIDs)
		}
	}
}

func TestParseDefinitionVersionOneKeepsEnglishAndThirtyDays(t *testing.T) {
	raw := `{"version":1,"sections":["hosts_offline","hosts_offline","open_alerts"],"host_group_ids":[" g1 ","","g1","g2"],"limits":{"top_hosts":5}}`
	def, err := ParseDefinition([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if def.Version != 2 || def.Language != "en" || def.PeriodDays != 30 {
		t.Fatalf("v1 defaults not applied: %+v", def)
	}
	if strings.Join(def.Sections, ",") != "hosts_offline,open_alerts" {
		t.Fatalf("sections not deduped in order: %v", def.Sections)
	}
	if strings.Join(def.HostGroupIDs, ",") != "g1,g2" {
		t.Fatalf("group ids not normalised: %v", def.HostGroupIDs)
	}
	if def.Limits.TopHosts != 5 {
		t.Fatalf("top hosts %d", def.Limits.TopHosts)
	}
}

func TestParseDefinitionVersionTwo(t *testing.T) {
	raw := `{"version":2,"sections":["host_overview","disks","reboots"],"language":"de","period_days":90}`
	def, err := ParseDefinition([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if def.Language != "de" || def.PeriodDays != 90 || len(def.Sections) != 3 {
		t.Fatalf("%+v", def)
	}
}

func TestParseDefinitionRejects(t *testing.T) {
	cases := map[string]string{
		"invalid json":    `{"version":`,
		"unknown version": `{"version":3}`,
		"unknown section": `{"sections":["executive_summary","pdf_magic"]}`,
		"language fr":     `{"language":"fr"}`,
		"period 14":       `{"period_days":14}`,
		"top hosts 500":   `{"limits":{"top_hosts":500}}`,
		"top hosts -1":    `{"limits":{"top_hosts":-1}}`,
	}
	for name, raw := range cases {
		if _, err := ParseDefinition([]byte(raw)); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
	// 51 group ids
	ids := make([]string, 0, MaxGroupIDs+1)
	for i := 0; i <= MaxGroupIDs; i++ {
		ids = append(ids, `"g`+strings.Repeat("x", i%3)+string(rune('a'+i%26))+string(rune('a'+i/26))+`"`)
	}
	if _, err := ParseDefinition([]byte(`{"host_group_ids":[` + strings.Join(ids, ",") + `]}`)); err == nil {
		t.Errorf("51 groups: expected error")
	}
}

func TestKnownSectionsCoverEveryConstant(t *testing.T) {
	want := []string{
		SectionExecutiveSummary, SectionComplianceSummary, SectionRecentPatchRuns, SectionHostStatus,
		SectionOpenAlerts, SectionHostsByUpdates, SectionTopSecurityPackages, SectionHostOverview,
		SectionSecurityUpdatesByHost, SectionDisks, SectionPatchActivity, SectionReboots,
	}
	if len(KnownSections) != len(want) {
		t.Fatalf("KnownSections has %d entries, want %d", len(KnownSections), len(want))
	}
	for i, s := range want {
		if KnownSections[i] != s {
			t.Fatalf("KnownSections[%d] = %q, want %q", i, KnownSections[i], s)
		}
	}
}
