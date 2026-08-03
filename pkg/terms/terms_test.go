package terms

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultTechTerms(t *testing.T) {
	terms := DefaultTechTerms()
	if len(terms) == 0 {
		t.Fatal("expected non-empty default tech terms list")
	}

	// Verify some key terms exist
	expected := []string{"Go", "Kubernetes", "PostgreSQL", "gRPC", "TypeScript"}
	termSet := make(map[string]bool)
	for _, term := range terms {
		termSet[term] = true
	}

	for _, exp := range expected {
		if !termSet[exp] {
			t.Errorf("expected default terms to contain %q, but it was missing", exp)
		}
	}
}

func TestParseCustomTerms(t *testing.T) {
	input := []string{"Google", " Envoy, Alex ", "OpenAI,   Anthropic"}
	got := ParseCustomTerms(input)

	want := []string{"Google", "Envoy", "Alex", "OpenAI", "Anthropic"}
	if len(got) != len(want) {
		t.Fatalf("ParseCustomTerms len = %d, want %d (got: %v)", len(got), len(want), got)
	}

	for i, w := range want {
		if got[i] != w {
			t.Errorf("ParseCustomTerms[%d] = %q, want %q", i, got[i], w)
		}
	}
}

func TestLoadTermsFromFile(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "terms.txt")

	content := "# Company and People Names\n\nEnvoy\nAlex\n# Comment\nSundar Pichai, Satya Nadella\n"
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test terms file: %v", err)
	}

	got, err := LoadTermsFromFile(filePath)
	if err != nil {
		t.Fatalf("LoadTermsFromFile failed: %v", err)
	}

	want := []string{"Envoy", "Alex", "Sundar Pichai", "Satya Nadella"}
	if len(got) != len(want) {
		t.Fatalf("LoadTermsFromFile len = %d, want %d (got: %v)", len(got), len(want), got)
	}

	for i, w := range want {
		if got[i] != w {
			t.Errorf("LoadTermsFromFile[%d] = %q, want %q", i, got[i], w)
		}
	}
}

func TestCombineTerms(t *testing.T) {
	defaults := []string{"Go", "Kubernetes", "React"}
	custom := []string{"Envoy", "Go", "Alex", "react"} // "Go" duplicate case-insensitive check or preserve exact casing

	combined := CombineTerms(defaults, custom)

	// Check deduplication
	seen := make(map[string]int)
	for _, term := range combined {
		seen[term]++
	}

	if seen["Go"] != 1 {
		t.Errorf("expected 'Go' to appear once, got %d", seen["Go"])
	}

	if !contains(combined, "Envoy") || !contains(combined, "Alex") || !contains(combined, "Kubernetes") {
		t.Errorf("missing expected terms in combined list: %v", combined)
	}
}

func contains(slice []string, val string) bool {
	for _, s := range slice {
		if s == val {
			return true
		}
	}
	return false
}
