package db

import "testing"

func TestLatestEmbeddedVersion(t *testing.T) {
	v, err := latestEmbeddedVersion()
	if err != nil {
		t.Fatal(err)
	}
	if v != 1 {
		t.Fatalf("latest embedded version = %d, want 1", v)
	}
}
