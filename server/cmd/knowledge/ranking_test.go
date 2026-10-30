package main

import "testing"

func TestRankCandidatesUsesExplicitServiceNames(t *testing.T) {
	items := []retrievalCandidate{
		{source: "docker-toolkit/elasticsearch/sample.env", content: "ELASTICSEARCH_HOST=localhost", vectorScore: 0.58},
		{source: "docker-toolkit/README.md", content: "PHP + Nginx + MySQL + Redis 可按需选择 Compose 模板", vectorScore: 0.54},
	}
	result := rankCandidates("PHP Nginx MySQL Redis compose 模板", items, 2, 0)
	if result[0].Metadata["source"] != "docker-toolkit/README.md" {
		t.Fatalf("expected multi-service guide first, got %v", result[0].Metadata["source"])
	}
}

func TestRankCandidatesPrefersRequestedVersion(t *testing.T) {
	items := []retrievalCandidate{
		{source: "docker-toolkit/php/8.5/sample.env", content: "PHP_EXTENSIONS=redis,gd", vectorScore: 0.62},
		{source: "docker-toolkit/php/8.2/sample.env", content: "PHP_EXTENSIONS=redis,gd", vectorScore: 0.58},
	}
	result := rankCandidates("PHP 8.2 安装 redis 和 gd 扩展", items, 2, 0)
	if result[0].Metadata["source"] != "docker-toolkit/php/8.2/sample.env" {
		t.Fatalf("expected requested version first, got %v", result[0].Metadata["source"])
	}
}
