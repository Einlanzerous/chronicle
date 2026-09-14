package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Einlanzerous/chronicle/internal/config"
)

// CHRN-100: serve refuses to stand behind a tier-1 corpus that is not there,
// in both ways it can fail to be — unset and missing — and accepts an empty
// one, because "not generated yet" is the truth on a fresh host.

func TestOpenEstateWikiRefusesAnUnsetMount(t *testing.T) {
	_, err := openEstateWiki(config.Config{})
	if err == nil {
		t.Fatal("an unset CHRONICLE_TIER1_WIKI_DIR was accepted")
	}
	if !strings.Contains(err.Error(), "CHRONICLE_TIER1_WIKI_DIR") {
		t.Fatalf("the refusal does not name the variable: %v", err)
	}
}

func TestOpenEstateWikiRefusesAMissingMount(t *testing.T) {
	_, err := openEstateWiki(config.Config{Tier1WikiDir: filepath.Join(t.TempDir(), "absent")})
	if err == nil {
		t.Fatal("a missing mount was accepted")
	}
	if !strings.Contains(err.Error(), "CHRONICLE_TIER1_WIKI_DIR") {
		t.Fatalf("the refusal does not name the variable: %v", err)
	}
}

func TestOpenEstateWikiRefusesAFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := openEstateWiki(config.Config{Tier1WikiDir: file}); err == nil {
		t.Fatal("a regular file was accepted as the mount")
	}
}

func TestOpenEstateWikiAcceptsAnEmptyMount(t *testing.T) {
	root := t.TempDir()
	corpus, err := openEstateWiki(config.Config{Tier1WikiDir: root})
	if err != nil {
		t.Fatal(err)
	}
	if corpus.Root() != root {
		t.Fatalf("root = %q, want %q", corpus.Root(), root)
	}
}
