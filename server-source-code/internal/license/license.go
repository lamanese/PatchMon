// Package license resolves the effective host licence (fork feature:
// amanit sells packages by VM count). The licence is comfort + transparency
// + contractual documentation, not copy protection: the limit is visible to
// both sides and optionally blocks new host registrations.
package license

import (
	"github.com/PatchMon/PatchMon/server-source-code/internal/config"
	"github.com/PatchMon/PatchMon/server-source-code/internal/models"
)

// TolerancePct is the contractual tolerance on top of the licensed host
// count (matches the price list): the hard limit is ceil(max * 1.1).
const TolerancePct = 10

// MaxLicensableHosts caps license_max_hosts (API validation and env
// clamp). Far above any real package; keeps the value safely inside int32
// and HardLimit's arithmetic.
const MaxLicensableHosts = 1_000_000

// Status values reported by GET /api/v1/license.
const (
	StatusUnlicensed    = "unlicensed"
	StatusOK            = "ok"
	StatusOverLimit     = "over_limit"
	StatusOverTolerance = "over_tolerance"
)

// Effective is the resolved licence: env override (PM_LICENSE_*) wins over
// the DB settings and locks UI editing.
type Effective struct {
	MaxHosts *int
	Enforce  bool
	Package  *string
	Locked   bool
}

// Resolve returns the effective licence. When PM_LICENSE_MAX_HOSTS is set
// (> 0), all three values come from the env and the settings are locked;
// otherwise the DB settings apply. s may be nil (treated as unlicensed).
func Resolve(cfg *config.Config, s *models.Settings) Effective {
	if cfg != nil && cfg.LicenseMaxHosts > 0 {
		max := cfg.LicenseMaxHosts
		if max > MaxLicensableHosts {
			max = MaxLicensableHosts
		}
		eff := Effective{MaxHosts: &max, Enforce: cfg.LicenseEnforce, Locked: true}
		if cfg.LicensePackage != "" {
			pkg := cfg.LicensePackage
			eff.Package = &pkg
		}
		return eff
	}
	if s == nil {
		return Effective{}
	}
	eff := Effective{Enforce: s.LicenseEnforce, Package: s.LicensePackage}
	if s.LicenseMaxHosts != nil && *s.LicenseMaxHosts > 0 {
		max := *s.LicenseMaxHosts
		eff.MaxHosts = &max
	}
	return eff
}

// HardLimit returns the blocking threshold ceil(max * (100+TolerancePct)/100).
// Host creation is refused once active+pending reaches this value, so the
// last host that can be created is number HardLimit(max).
func HardLimit(max int) int {
	return (max*(100+TolerancePct) + 99) / 100
}

// Status classifies the slot usage (active+pending) against the licence.
// Thresholds match the gate so the banner always explains a 403:
// ok while used <= max, over_limit (yellow) above max, over_tolerance (red)
// once the full tolerance is consumed.
func (e Effective) Status(used int) string {
	if e.MaxHosts == nil {
		return StatusUnlicensed
	}
	switch {
	case used <= *e.MaxHosts:
		return StatusOK
	case used < HardLimit(*e.MaxHosts):
		return StatusOverLimit
	default:
		return StatusOverTolerance
	}
}

// Blocks reports whether creating another host must be refused: enforcement
// on and the slot usage (active+pending) has consumed max plus tolerance.
func (e Effective) Blocks(used int) bool {
	return e.Enforce && e.MaxHosts != nil && used >= HardLimit(*e.MaxHosts)
}
