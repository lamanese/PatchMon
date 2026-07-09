package license

import (
	"testing"

	"github.com/PatchMon/PatchMon/server-source-code/internal/config"
	"github.com/PatchMon/PatchMon/server-source-code/internal/models"
)

func intPtr(v int) *int { return &v }

func TestHardLimit(t *testing.T) {
	cases := map[int]int{
		50:   55,
		100:  110,
		200:  220,
		25:   28, // ceil(27.5) — tolerance rounds up
		1:    2,  // ceil(1.1)
		3:    4,  // ceil(3.3)
		1000: 1100,
	}
	for max, want := range cases {
		if got := HardLimit(max); got != want {
			t.Errorf("HardLimit(%d) = %d, want %d", max, got, want)
		}
	}
}

func TestResolveEnvOverrideWins(t *testing.T) {
	cfg := &config.Config{LicenseMaxHosts: 200, LicenseEnforce: true, LicensePackage: "Paket 3"}
	s := &models.Settings{LicenseMaxHosts: intPtr(50), LicenseEnforce: false}
	eff := Resolve(cfg, s)
	if !eff.Locked || eff.MaxHosts == nil || *eff.MaxHosts != 200 || !eff.Enforce {
		t.Fatalf("env override not applied: %+v", eff)
	}
	if eff.Package == nil || *eff.Package != "Paket 3" {
		t.Fatalf("package not taken from env: %+v", eff)
	}
}

func TestResolveEnvLocksEnforceOff(t *testing.T) {
	// Env max set but enforce not set: DB enforce must NOT leak through.
	cfg := &config.Config{LicenseMaxHosts: 200}
	s := &models.Settings{LicenseMaxHosts: intPtr(50), LicenseEnforce: true}
	eff := Resolve(cfg, s)
	if eff.Enforce {
		t.Fatal("DB enforce leaked through env override")
	}
	if !eff.Locked {
		t.Fatal("expected locked")
	}
}

func TestResolveEnvClampedToCap(t *testing.T) {
	cfg := &config.Config{LicenseMaxHosts: MaxLicensableHosts * 10}
	eff := Resolve(cfg, nil)
	if eff.MaxHosts == nil || *eff.MaxHosts != MaxLicensableHosts {
		t.Fatalf("env max not clamped: %+v", eff)
	}
	if HardLimit(*eff.MaxHosts) <= *eff.MaxHosts {
		t.Fatal("hard limit must stay above max after clamping")
	}
}

func TestResolveFromSettings(t *testing.T) {
	pkg := "Paket 1"
	s := &models.Settings{LicenseMaxHosts: intPtr(50), LicenseEnforce: true, LicensePackage: &pkg}
	eff := Resolve(&config.Config{}, s)
	if eff.Locked || eff.MaxHosts == nil || *eff.MaxHosts != 50 || !eff.Enforce {
		t.Fatalf("settings not applied: %+v", eff)
	}
}

func TestResolveUnlicensed(t *testing.T) {
	if eff := Resolve(&config.Config{}, &models.Settings{}); eff.MaxHosts != nil {
		t.Fatalf("expected unlicensed, got %+v", eff)
	}
	if eff := Resolve(nil, nil); eff.MaxHosts != nil || eff.Enforce || eff.Locked {
		t.Fatalf("nil inputs must be unlicensed: %+v", eff)
	}
	// Zero or negative DB values mean "no limit".
	if eff := Resolve(&config.Config{}, &models.Settings{LicenseMaxHosts: intPtr(0)}); eff.MaxHosts != nil {
		t.Fatalf("zero max must be unlicensed: %+v", eff)
	}
}

func TestStatusThresholds(t *testing.T) {
	eff := Effective{MaxHosts: intPtr(50)}
	cases := []struct {
		used int
		want string
	}{
		{0, StatusOK},
		{50, StatusOK},
		{51, StatusOverLimit},
		{54, StatusOverLimit},
		{55, StatusOverTolerance},
		{60, StatusOverTolerance},
	}
	for _, c := range cases {
		if got := eff.Status(c.used); got != c.want {
			t.Errorf("Status(%d) = %s, want %s", c.used, got, c.want)
		}
	}
	if got := (Effective{}).Status(10); got != StatusUnlicensed {
		t.Errorf("Status without max = %s, want unlicensed", got)
	}
}

func TestBlocks(t *testing.T) {
	enforced := Effective{MaxHosts: intPtr(50), Enforce: true}
	if enforced.Blocks(54) {
		t.Error("must not block below hard limit (54 < 55)")
	}
	if !enforced.Blocks(55) {
		t.Error("must block at hard limit (55th slot used, 56th refused)")
	}
	if (Effective{MaxHosts: intPtr(50)}).Blocks(100) {
		t.Error("must not block when enforce is off")
	}
	if (Effective{Enforce: true}).Blocks(100) {
		t.Error("must not block without a max")
	}
}
