package main

import "fmt"

// byteUnit is the step between size units, matching how audio tools report file
// sizes.
const byteUnit = 1024

// formatFileSize renders a byte count the way a person would read it.
func formatFileSize(bytes int64) string {
	if bytes < byteUnit {
		return fmt.Sprintf("%d B", bytes)
	}

	div, exp := int64(byteUnit), 0
	for n := bytes / byteUnit; n >= byteUnit; n /= byteUnit {
		div *= byteUnit
		exp++
	}

	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
