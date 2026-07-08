package handler

import (
	"context"
	"log/slog"

	"github.com/PatchMon/PatchMon/server-source-code/internal/store"
)

// NewComplianceProgressHandler returns an OnComplianceProgress callback that
// resolves "running" compliance scan placeholders when the agent reports a
// terminal scan phase over its WebSocket. A scan that fails before producing
// any results (e.g. OpenSCAP unavailable on the host) never reaches the
// results endpoint, so without this the placeholder rows would show as
// "in progress" until the nightly stalled-scan cleanup fails them.
func NewComplianceProgressHandler(hosts *store.HostsStore, compliance *store.ComplianceStore, log *slog.Logger) OnComplianceProgress {
	return func(ctx context.Context, apiID, phase, errorMessage string) {
		if phase != "failed" && phase != "cancelled" {
			return
		}
		host, err := hosts.GetByApiID(ctx, apiID)
		if err != nil || host == nil {
			log.Debug("compliance progress: could not resolve host, dropping terminal phase",
				"api_id", apiID, "phase", phase, "error", err)
			return
		}
		msg := errorMessage
		// Agent-controlled text: cap it so a misbehaving agent cannot bloat
		// the scan list (WS messages may be up to 512KB).
		if len(msg) > 2000 {
			msg = msg[:2000] + "…"
		}
		if msg == "" {
			if phase == "cancelled" {
				msg = "Scan cancelled"
			} else {
				msg = "Scan failed (agent reported no details)"
			}
		}
		if err := compliance.FailRunningScans(ctx, host.ID, msg); err != nil {
			log.Warn("failed to resolve running compliance scans after agent-reported terminal phase",
				"host_id", host.ID, "phase", phase, "error", err)
		}
	}
}
