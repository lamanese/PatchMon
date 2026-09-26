package reports

import (
	"regexp"
	"strconv"
)

// DiskUsage is the numeric content of the agent's formatted disk size string.
type DiskUsage struct {
	TotalGB     float64
	UsedGB      float64
	FreeGB      float64
	UsedPercent float64
}

// Disk usage thresholds for the report markers.
const (
	DiskWarnPercent     = 85
	DiskCriticalPercent = 95
)

// diskSizeRe matches the agent format written in hardware.go:
// "49.10GB (12.30GB used, 36.80GB free, 26.5% used)".
var diskSizeRe = regexp.MustCompile(`^\s*([0-9]+(?:\.[0-9]+)?)GB \(([0-9]+(?:\.[0-9]+)?)GB used, ([0-9]+(?:\.[0-9]+)?)GB free, ([0-9]+(?:\.[0-9]+)?)% used\)\s*$`)

// ParseDiskSize parses the agent's formatted size string. Anything that does
// not match the format exactly returns ok=false; callers then show the raw text.
func ParseDiskSize(s string) (DiskUsage, bool) {
	m := diskSizeRe.FindStringSubmatch(s)
	if m == nil {
		return DiskUsage{}, false
	}
	var vals [4]float64
	for i := range vals {
		v, err := strconv.ParseFloat(m[i+1], 64)
		if err != nil {
			return DiskUsage{}, false
		}
		vals[i] = v
	}
	return DiskUsage{TotalGB: vals[0], UsedGB: vals[1], FreeGB: vals[2], UsedPercent: vals[3]}, true
}

// DiskLevel classifies a usage percentage: "" (fine), "warn" (>= 85 %),
// "critical" (>= 95 %).
func DiskLevel(pct float64) string {
	switch {
	case pct >= DiskCriticalPercent:
		return "critical"
	case pct >= DiskWarnPercent:
		return "warn"
	default:
		return ""
	}
}
