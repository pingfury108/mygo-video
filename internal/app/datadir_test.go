package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDataDirPointerIsFoundAfterRestart(t *testing.T) {
	defaultDir := t.TempDir()
	target := t.TempDir()

	if err := writeDataDirPointerAt(defaultDir, target); err != nil {
		t.Fatal(err)
	}
	if got := resolveBaseDir(defaultDir); !sameDir(got, target) {
		t.Fatalf("resolveBaseDir() = %q, want %q", got, target)
	}
}

func TestDataDirPointerFollowsExistingChain(t *testing.T) {
	defaultDir := t.TempDir()
	older := t.TempDir()
	target := t.TempDir()

	if err := writeDataDirPointerAt(defaultDir, older); err != nil {
		t.Fatal(err)
	}
	if err := writeDataDirPointerAt(older, target); err != nil {
		t.Fatal(err)
	}
	if got := resolveBaseDir(defaultDir); !sameDir(got, target) {
		t.Fatalf("resolveBaseDir() = %q, want %q", got, target)
	}
}

func TestDataDirPointerKeepsBootstrapFileSmall(t *testing.T) {
	defaultDir := t.TempDir()
	target := t.TempDir()

	if err := writeDataDirPointerAt(defaultDir, target); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(defaultDir, pointerName))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != target+"\n" {
		t.Fatalf("pointer = %q, want %q", b, target+"\n")
	}
}
