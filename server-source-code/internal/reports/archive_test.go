package reports

import (
	"errors"
	"testing"
)

func TestValidateArchiveKeep(t *testing.T) {
	for _, n := range []int{1, 24, 200} {
		if err := ValidateArchiveKeep(n); err != nil {
			t.Fatalf("%d: %v", n, err)
		}
	}
	for _, n := range []int{0, -1, 201, 100000} {
		if err := ValidateArchiveKeep(n); !errors.Is(err, ErrArchiveKeep) {
			t.Fatalf("%d: want ErrArchiveKeep, got %v", n, err)
		}
	}
}
