package reports

import "testing"

func TestPackagesText(t *testing.T) {
	one := "nginx"
	if got := packagesText(&one, []byte(`["a","b"]`)); got != "nginx" {
		t.Fatalf("single name wins: %q", got)
	}
	if got := packagesText(nil, []byte(`["a","b","c"]`)); got != "a, b, c" {
		t.Fatalf("three names: %q", got)
	}
	if got := packagesText(nil, []byte(`["a","b","c","d","e"]`)); got != "a, b, c +2" {
		t.Fatalf("more than three: %q", got)
	}
	if got := packagesText(nil, nil); got != "" {
		t.Fatalf("empty means all packages, rendered by the template: %q", got)
	}
	if got := packagesText(nil, []byte(`not json`)); got != "" {
		t.Fatalf("invalid json: %q", got)
	}
}
