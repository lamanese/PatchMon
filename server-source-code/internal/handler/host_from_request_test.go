package handler

import (
	"net/http/httptest"
	"testing"

	hostctx "github.com/PatchMon/PatchMon/server-source-code/internal/context"
)

// Job payloads must carry a tenant identity only when the request was
// resolved to a tenant. A raw X-Forwarded-Host on a single-instance server
// behind a proxy is not a tenant and would make fail-closed workers reject
// the task.
func TestHostFromRequestUsesResolvedTenantNotRawHeader(t *testing.T) {
	r := httptest.NewRequest("POST", "/x", nil)
	r.Header.Set("X-Forwarded-Host", "dev-patchmon.example")
	if got := hostFromRequest(r); got != "" {
		t.Fatalf("single-instance request must yield no tenant host, got %q", got)
	}
	r = r.WithContext(hostctx.WithEntry(r.Context(), &hostctx.Entry{Host: "tenant.example"}))
	if got := hostFromRequest(r); got != "tenant.example" {
		t.Fatalf("resolved tenant expected, got %q", got)
	}
}
