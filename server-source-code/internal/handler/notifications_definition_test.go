package handler

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/PatchMon/PatchMon/server-source-code/internal/reports"
)

func TestDefinitionErrorStatus(t *testing.T) {
	if got := definitionErrorStatus(fmt.Errorf("x: %w", reports.ErrDefinition)); got != http.StatusBadRequest {
		t.Fatalf("definition error → %d", got)
	}
	if got := definitionErrorStatus(fmt.Errorf("x: %w", reports.ErrScopeInvalid)); got != http.StatusBadRequest {
		t.Fatalf("scope error → %d", got)
	}
	if got := definitionErrorStatus(errors.New("load host groups: connection refused")); got != http.StatusInternalServerError {
		t.Fatalf("database error must not be a 400, got %d", got)
	}
}
