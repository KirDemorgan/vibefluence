package index

import (
	"sort"
	"strings"
)

const (
	titleWeight = 10
	pathWeight  = 4
	maxBodyHits = 10
)

type SearchResult struct {
	Repo    string
	Path    string
	Title   string
	Type    string
	Snippet string
	Score   int
}

func terms(query string) []string {
	return strings.Fields(strings.ToLower(query))
}

func (s *Snapshot) Search(query string, limit int) []SearchResult {
	words := terms(query)
	if len(words) == 0 {
		return nil
	}
	var results []SearchResult
	for _, doc := range s.Docs {
		if result, ok := score(doc, words); ok {
			results = append(results, result)
		}
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		return results[i].Path < results[j].Path
	})
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	return results
}

func score(doc *Doc, words []string) (SearchResult, bool) {
	total := 0
	snippetOffset, snippetLength := -1, 0
	for _, word := range words {
		inTitle := strings.Contains(doc.lowerTitle, word)
		inPath := strings.Contains(doc.lowerPath, word)
		bodyHits := strings.Count(doc.lowerBody, word)
		if !inTitle && !inPath && bodyHits == 0 {
			return SearchResult{}, false
		}
		if inTitle {
			total += titleWeight
		}
		if inPath {
			total += pathWeight
		}
		total += min(bodyHits, maxBodyHits)
		if bodyHits > 0 && snippetOffset < 0 {
			snippetOffset, snippetLength = strings.Index(doc.lowerBody, word), len(word)
		}
	}
	result := SearchResult{Repo: doc.Repo, Path: doc.Path, Title: doc.Title, Type: doc.Type, Score: total}
	switch {
	case snippetOffset >= 0 && len(doc.lowerBody) == len(doc.Body):
		result.Snippet = snippetAround(doc.Body, snippetOffset, snippetLength)
	case snippetOffset >= 0:
		result.Snippet = snippetAround(doc.lowerBody, snippetOffset, snippetLength)
	}
	return result, true
}
