package main

import (
	"strings"
	"testing"
)

func TestSplitMarkdownPreservesOverlapAndTail(t *testing.T) {
	source := strings.Repeat("部署检查。", 210) + "最终恢复步骤。"
	parts := splitMarkdown(source, 850, 100)
	if len(parts) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(parts))
	}
	if !strings.Contains(parts[len(parts)-1], "最终恢复步骤。") {
		t.Fatal("last chunk lost the document tail")
	}
	if !strings.Contains(parts[1], string([]rune(parts[0])[len([]rune(parts[0]))-100:])) {
		t.Fatal("overlap between adjacent chunks is missing")
	}
}
