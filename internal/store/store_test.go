package store

import "testing"

func TestSourceKept(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.SetSource(3)
	again, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := again.Source(); got != 3 {
		t.Fatalf("source = %d, want 3", got)
	}
}
