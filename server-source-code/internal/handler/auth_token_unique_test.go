package handler

import (
	"testing"

	"github.com/PatchMon/PatchMon/server-source-code/internal/config"
)

// Two sign-ins by the same user inside one second used to mint byte-identical
// refresh tokens, because every claim (sub, role, typ, exp, iat) has second
// granularity. The second insert then hit the unique index on
// user_sessions.refresh_token and the login answered 500.
func TestRefreshTokensAreUniqueWithinOneSecond(t *testing.T) {
	h := &AuthHandler{cfg: &config.Config{JWTSecret: "test-secret-test-secret-test-secret"}}
	seen := make(map[string]struct{}, 50)
	for i := 0; i < 50; i++ {
		tok, err := h.createRefreshToken("user-1", "admin", 3600)
		if err != nil {
			t.Fatalf("createRefreshToken: %v", err)
		}
		if _, dup := seen[tok]; dup {
			t.Fatalf("token %d duplicates an earlier one minted for the same user", i)
		}
		seen[tok] = struct{}{}
	}
}
