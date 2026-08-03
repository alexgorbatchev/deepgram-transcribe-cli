package terms

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// ParseCustomTerms takes raw flag values (e.g. ["Envoy", "Alex, OpenAI"]) and splits,
// trims, and cleans them into individual terms.
func ParseCustomTerms(rawTerms []string) []string {
	var result []string
	for _, raw := range rawTerms {
		parts := strings.Split(raw, ",")
		for _, part := range parts {
			cleaned := strings.TrimSpace(part)
			if cleaned != "" {
				result = append(result, cleaned)
			}
		}
	}
	return result
}

// LoadTermsFromFile reads a text file line-by-line. Blank lines and lines starting
// with '#' are ignored. Comma-separated entries on a single line are split.
func LoadTermsFromFile(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening terms file %q: %w", path, err)
	}
	defer file.Close()

	var rawLines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		rawLines = append(rawLines, line)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading terms file %q: %w", path, err)
	}

	return ParseCustomTerms(rawLines), nil
}

// CombineTerms combines multiple slices of terms and removes duplicates while preserving case/order.
func CombineTerms(termLists ...[]string) []string {
	seen := make(map[string]bool)
	var combined []string

	for _, list := range termLists {
		for _, term := range list {
			cleaned := strings.TrimSpace(term)
			if cleaned == "" {
				continue
			}

			// Normalized key for case-insensitive deduplication
			normKey := strings.ToLower(cleaned)
			if !seen[normKey] {
				seen[normKey] = true
				combined = append(combined, cleaned)
			}
		}
	}

	return combined
}
