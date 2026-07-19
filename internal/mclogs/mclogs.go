// Package mclogs talks to the mclo.gs log analysis API (Aternos).
// Only the stateless /analyse endpoint is used: the log is inspected
// server-side but never stored or published.
package mclogs

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"

	"deckanator/internal/errs"
)

const analyseURL = "https://api.mclo.gs/1/analyse"

// Solution is a suggested fix for a detected problem.
type Solution struct {
	Message string `json:"message"`
}

// Problem is a known issue detected in the log.
type Problem struct {
	Message   string     `json:"message"`
	Solutions []Solution `json:"solutions"`
}

// Information is a neutral fact extracted from the log (game version,
// loader, etc.).
type Information struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// Analysis is the result of a log inspection.
type Analysis struct {
	Problems    []Problem     `json:"problems"`
	Information []Information `json:"information"`
}

// AnalyzeFiles analyzes each file and merges the results, deduplicating
// problems by message and information by label. Unreadable files are
// skipped; an API error is returned only if nothing was gathered.
func AnalyzeFiles(paths []string) (Analysis, error) {
	// API limit is 10 MiB / 25k lines; the tail is what matters.
	const maxBytes = 1 << 20
	var merged Analysis
	seenProblems := map[string]bool{}
	seenInfo := map[string]bool{}
	var lastErr error
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if len(data) > maxBytes {
			data = data[len(data)-maxBytes:]
		}
		res, err := Analyze(string(data))
		if err != nil {
			lastErr = err
			continue
		}
		for _, p := range res.Problems {
			if !seenProblems[p.Message] {
				seenProblems[p.Message] = true
				merged.Problems = append(merged.Problems, p)
			}
		}
		for _, in := range res.Information {
			if !seenInfo[in.Label] {
				seenInfo[in.Label] = true
				merged.Information = append(merged.Information, in)
			}
		}
	}
	if len(merged.Problems) == 0 && len(merged.Information) == 0 && lastErr != nil {
		return Analysis{}, lastErr
	}
	return merged, nil
}

// Analyze sends log content to mclo.gs and returns detected problems
// with human-readable explanations and suggested solutions.
func Analyze(content string) (_ Analysis, e error) {
	resp, err := http.PostForm(analyseURL, url.Values{"content": {content}})
	if err != nil {
		return Analysis{}, err
	}
	defer errs.Close(&e, resp.Body)

	var raw struct {
		Success     bool          `json:"success"`
		Error       string        `json:"error"`
		Analysis    *Analysis     `json:"analysis"`
		Problems    []Problem     `json:"problems"`
		Information []Information `json:"information"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return Analysis{}, err
	}
	if raw.Error != "" {
		return Analysis{}, fmt.Errorf("mclo.gs: %s", raw.Error)
	}
	if raw.Analysis != nil {
		return *raw.Analysis, nil
	}
	return Analysis{Problems: raw.Problems, Information: raw.Information}, nil
}
