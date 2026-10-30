package main

import (
	"math"
	"regexp"
	"sort"
	"strings"
)

type retrievalCandidate struct {
	content     string
	title       string
	source      string
	vectorScore float64
}

var technicalTermPattern = regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9_.-]*`)
var versionPattern = regexp.MustCompile(`\b[0-9]+(?:\.[0-9]+)+\b`)
var sourceVersionPattern = regexp.MustCompile(`/(?:php|nginx|mysql|redis|postgres|elasticsearch|kibana|mongo|rabbitmq)/([0-9]+(?:\.[0-9]+)*)/`)

// rankCandidates 将向量相似度与问题中明确写出的技术名称结合，避免多服务问题
// 被含有大量通用环境变量的无关模板占满；同一文件优先最多返回两个片段。
func rankCandidates(query string, candidates []retrievalCandidate, topK int, threshold float64) []record {
	terms := distinctTerms(query)
	wantedVersion := versionPattern.FindString(query)
	type ranked struct {
		record
		source string
	}
	items := make([]ranked, 0, len(candidates))
	for _, candidate := range candidates {
		score := candidate.vectorScore
		if len(terms) > 0 {
			body := strings.ToLower(candidate.content)
			path := strings.ToLower(candidate.source)
			matches, pathMatches := 0, 0
			for _, term := range terms {
				if strings.Contains(body, term) || strings.Contains(path, term) {
					matches++
				}
				if strings.Contains(path, term) {
					pathMatches++
				}
			}
			score += 0.24*float64(matches)/float64(len(terms)) + 0.08*float64(pathMatches)/float64(len(terms))
		}
		if wantedVersion != "" {
			if found := sourceVersionPattern.FindStringSubmatch("/" + candidate.source); len(found) == 2 {
				if found[1] == wantedVersion {
					score += 0.12
				} else {
					score -= 0.16
				}
			}
		}
		score = math.Max(0, math.Min(1, score))
		if score < threshold {
			continue
		}
		items = append(items, ranked{record: record{
			Content: candidate.content, Score: score, Title: candidate.title,
			Metadata: map[string]any{"source": candidate.source},
		}, source: candidate.source})
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Score > items[j].Score })
	result := make([]record, 0, topK)
	perSource := make(map[string]int)
	for _, item := range items {
		if perSource[item.source] >= 2 {
			continue
		}
		result = append(result, item.record)
		perSource[item.source]++
		if len(result) == topK {
			return result
		}
	}
	return result
}

func distinctTerms(query string) []string {
	seen := make(map[string]bool)
	var terms []string
	for _, raw := range technicalTermPattern.FindAllString(query, -1) {
		term := strings.ToLower(raw)
		if len(term) < 2 || seen[term] || term == "docker" {
			continue
		}
		seen[term] = true
		terms = append(terms, term)
	}
	return terms
}
