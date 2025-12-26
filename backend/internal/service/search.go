package service

import (
	"context"
	"sort"
	"strings"
	"sync"

	"searchav/internal/config"
	"searchav/internal/model"
	"searchav/internal/source"

	"github.com/rs/zerolog"
)

// SearchService handles video search aggregation
type SearchService struct {
	config *config.Config
	client *source.Client
	logger *zerolog.Logger
}

// NewSearchService creates a new search service
func NewSearchService(cfg *config.Config, client *source.Client, logger *zerolog.Logger) *SearchService {
	return &SearchService{
		config: cfg,
		client: client,
		logger: logger,
	}
}

// sourceResult holds result from a single source
type sourceResult struct {
	source config.SourceItem
	list   []source.RawVideo
	err    error
}

// Search performs aggregated search across all sources
func (s *SearchService) Search(ctx context.Context, keyword string, includeAdult bool) ([]model.VideoItem, error) {
	allSources := s.config.GetEnabledSources()

	// Filter sources based on adult flag
	var sources []config.SourceItem
	for _, src := range allSources {
		if includeAdult || !src.Adult {
			sources = append(sources, src)
		}
	}

	s.logger.Info().Int("sources", len(sources)).Str("keyword", keyword).Bool("adult", includeAdult).Msg("starting aggregated search")

	if len(sources) == 0 {
		s.logger.Warn().Msg("no enabled sources")
		return nil, nil
	}

	results := make(chan sourceResult, len(sources))
	var wg sync.WaitGroup

	// Concurrent requests to all sources
	for _, src := range sources {
		wg.Add(1)
		go func(src config.SourceItem) {
			defer wg.Done()
			s.logger.Info().Str("source", src.Code).Str("url", src.URL).Msg("requesting source")

			list, err := s.client.SearchWithTimeout(ctx, src, keyword, s.config.Source.Timeout)
			results <- sourceResult{source: src, list: list, err: err}
		}(src)
	}

	// Wait for all requests and close channel
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect results
	var allResults []source.RawVideo
	for r := range results {
		if r.err != nil {
			s.logger.Warn().Err(r.err).Str("source", r.source.Code).Msg("source request failed")
			continue
		}
		s.logger.Info().Str("source", r.source.Code).Int("count", len(r.list)).Msg("source returned results")
		allResults = append(allResults, r.list...)
	}

	s.logger.Info().Int("total", len(allResults)).Msg("collection complete, starting merge")

	// Merge and deduplicate
	merged := s.mergeResults(allResults)
	s.logger.Info().Int("merged", len(merged)).Msg("merge complete")

	// Sort by relevance
	s.sortByRelevance(merged, keyword)
	s.logger.Info().Msg("sort complete")

	return merged, nil
}

// mergeResults merges and deduplicates search results
func (s *SearchService) mergeResults(raw []source.RawVideo) []model.VideoItem {
	merged := make(map[string]*model.VideoItem)

	for _, v := range raw {
		key := strings.TrimSpace(v.VodName)
		if key == "" {
			continue
		}

		if item, ok := merged[key]; ok {
			// Exists, append source
			item.Sources = append(item.Sources, model.SourceInfo{
				SourceCode: v.SourceCode,
				SourceName: v.SourceName,
				VodID:      v.VodID,
			})
		} else {
			// New entry
			merged[key] = &model.VideoItem{
				VodName:    v.VodName,
				VodPic:     v.VodPic,
				VodRemarks: v.VodRemarks,
				TypeName:   v.TypeName,
				Sources: []model.SourceInfo{{
					SourceCode: v.SourceCode,
					SourceName: v.SourceName,
					VodID:      v.VodID,
				}},
			}
		}
	}

	// Convert map to slice
	result := make([]model.VideoItem, 0, len(merged))
	for _, v := range merged {
		result = append(result, *v)
	}
	return result
}

// sortByRelevance sorts results by match relevance to keyword
// Priority: exact match > prefix match > contains match
// Secondary: more sources > fewer sources
// Tertiary: shorter name > longer name
func (s *SearchService) sortByRelevance(items []model.VideoItem, keyword string) {
	keywordLower := strings.ToLower(strings.TrimSpace(keyword))

	sort.Slice(items, func(i, j int) bool {
		scoreI := s.calculateRelevanceScore(items[i].VodName, keywordLower)
		scoreJ := s.calculateRelevanceScore(items[j].VodName, keywordLower)

		if scoreI != scoreJ {
			return scoreI > scoreJ // Higher score first
		}

		// Same relevance, prefer more sources
		if len(items[i].Sources) != len(items[j].Sources) {
			return len(items[i].Sources) > len(items[j].Sources)
		}

		// Same sources count, prefer shorter name (more precise match)
		return len(items[i].VodName) < len(items[j].VodName)
	})
}

// calculateRelevanceScore returns a score based on how well the name matches the keyword
// Higher score = better match
func (s *SearchService) calculateRelevanceScore(name, keywordLower string) int {
	nameLower := strings.ToLower(strings.TrimSpace(name))

	// Exact match (highest priority)
	if nameLower == keywordLower {
		return 100
	}

	// Starts with keyword
	if strings.HasPrefix(nameLower, keywordLower) {
		return 80
	}

	// Check if keyword appears at the beginning of a "segment"
	if strings.Contains(nameLower, keywordLower) {
		// Bonus if keyword is at the start after common prefixes
		idx := strings.Index(nameLower, keywordLower)
		if idx == 0 {
			return 80
		}
		// Earlier position = higher score
		return 60 - idx
	}

	// No match (shouldn't happen in search results, but just in case)
	return 0
}
