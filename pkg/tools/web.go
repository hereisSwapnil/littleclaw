package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"littleclaw/pkg/providers"
)

// --- Constants ---

const (
	webFetchMaxBytes    = 30_000 // ~30 KB of raw body before truncation
	WebFetchMaxChars    = 8_000  // characters of clean text returned to LLM
	webSearchMaxResults = 5      // max search result items to return
	httpTimeout         = 15 * time.Second
)

// --- HTML stripping ---

var (
	reScript   = regexp.MustCompile(`(?is)<(script|style)[^>]*>.*?</(script|style)>`)
	reTag      = regexp.MustCompile(`<[^>]+>`)
	reSpaces   = regexp.MustCompile(`[ \t]+`)
	reNewlines = regexp.MustCompile(`\n{3,}`)
)

func StripHTML(raw string) string {
	s := reScript.ReplaceAllString(raw, " ")
	s = reTag.ReplaceAllString(s, " ")
	// Decode common HTML entities
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	s = strings.ReplaceAll(s, "&quot;", `"`)
	s = strings.ReplaceAll(s, "&#39;", "'")
	s = strings.ReplaceAll(s, "&nbsp;", " ")
	s = reSpaces.ReplaceAllString(s, " ")
	// Normalise line breaks
	lines := strings.Split(s, "\n")
	var clean []string
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if t != "" {
			clean = append(clean, t)
		}
	}
	s = strings.Join(clean, "\n")
	s = reNewlines.ReplaceAllString(s, "\n\n")
	if len([]rune(s)) > WebFetchMaxChars {
		runes := []rune(s)
		s = string(runes[:WebFetchMaxChars]) + "\n\n[...truncated]"
	}
	return strings.TrimSpace(s)
}

// --- web_fetch implementation ---

func DoWebFetch(rawURL string) (string, error) {
	client := &http.Client{Timeout: httpTimeout}

	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Littleclaw/1.0; +https://github.com/littleclaw)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("server returned HTTP %d", resp.StatusCode)
	}

	// Read with a size cap to avoid OOM on huge pages
	limited := io.LimitReader(resp.Body, webFetchMaxBytes)
	body, err := io.ReadAll(limited)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	ct := resp.Header.Get("Content-Type")
	if strings.Contains(ct, "application/json") {
		// Pretty-print JSON for the LLM
		var buf bytes.Buffer
		if json.Indent(&buf, body, "", "  ") == nil {
			text := buf.String()
			if len([]rune(text)) > WebFetchMaxChars {
				text = string([]rune(text)[:WebFetchMaxChars]) + "\n\n[...truncated]"
			}
			return text, nil
		}
	}

	return StripHTML(string(body)), nil
}

// --- Tavily search ---

type tavilyRequest struct {
	APIKey            string   `json:"api_key"`
	Query             string   `json:"query"`
	SearchDepth       string   `json:"search_depth"`
	IncludeAnswer     bool     `json:"include_answer"`
	IncludeRawContent bool     `json:"include_raw_content"`
	MaxResults        int      `json:"max_results"`
	IncludeDomains    []string `json:"include_domains,omitempty"`
	ExcludeDomains    []string `json:"exclude_domains,omitempty"`
}

type TavilyResult struct {
	Title   string  `json:"title"`
	URL     string  `json:"url"`
	Content string  `json:"content"`
	Score   float64 `json:"score"`
}

type TavilyResponse struct {
	Answer  string         `json:"answer"`
	Results []TavilyResult `json:"results"`
}

// tavilySearch calls the Tavily API and returns (formatted results, isRateLimited, error).
func tavilySearch(ctx context.Context, apiKey, query string) (string, bool, error) {
	payload := tavilyRequest{
		APIKey:            apiKey,
		Query:             query,
		SearchDepth:       "basic",
		IncludeAnswer:     true,
		IncludeRawContent: false,
		MaxResults:        webSearchMaxResults,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", false, fmt.Errorf("failed to encode request: %w", err)
	}

	client := &http.Client{Timeout: httpTimeout}
	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.tavily.com/search", bytes.NewReader(body))
	if err != nil {
		return "", false, fmt.Errorf("failed to build Tavily request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", false, fmt.Errorf("Tavily request failed: %w", err)
	}
	defer resp.Body.Close()

	// 429 = rate limited
	if resp.StatusCode == 429 {
		return "", true, fmt.Errorf("Tavily rate limit reached (HTTP 429)")
	}
	// 401/403 = bad/missing key
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return "", false, fmt.Errorf("Tavily authentication failed (HTTP %d) — check your API key", resp.StatusCode)
	}
	if resp.StatusCode >= 400 {
		return "", false, fmt.Errorf("Tavily returned HTTP %d", resp.StatusCode)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", false, fmt.Errorf("failed to read Tavily response: %w", err)
	}

	var tavilyResp TavilyResponse
	if err := json.Unmarshal(respBody, &tavilyResp); err != nil {
		return "", false, fmt.Errorf("failed to parse Tavily response: %w", err)
	}

	return FormatTavilyResults(query, tavilyResp), false, nil
}

func FormatTavilyResults(query string, r TavilyResponse) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Web search results for: %q\n", query))
	sb.WriteString("Source: Tavily\n\n")

	if r.Answer != "" {
		sb.WriteString("QUICK ANSWER:\n")
		sb.WriteString(r.Answer)
		sb.WriteString("\n\n")
	}

	if len(r.Results) == 0 {
		sb.WriteString("No results found.\n")
		return sb.String()
	}

	sb.WriteString("TOP RESULTS:\n")
	for i, res := range r.Results {
		sb.WriteString(fmt.Sprintf("\n%d. %s\n", i+1, res.Title))
		sb.WriteString(fmt.Sprintf("   URL: %s\n", res.URL))
		if res.Content != "" {
			snippet := res.Content
			if len([]rune(snippet)) > 400 {
				snippet = string([]rune(snippet)[:400]) + "..."
			}
			sb.WriteString(fmt.Sprintf("   %s\n", snippet))
		}
	}

	sb.WriteString("\nTip: Use web_fetch with any URL above to read the full page content.")
	return sb.String()
}

// --- DuckDuckGo HTML scrape fallback ---

func duckDuckGoSearch(ctx context.Context, query string) (string, error) {
	searchURL := "https://html.duckduckgo.com/html/?q=" + url.QueryEscape(query)

	client := &http.Client{Timeout: httpTimeout}
	req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to build DDG request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Littleclaw/1.0)")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("DDG request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("DDG returned HTTP %d", resp.StatusCode)
	}

	limited := io.LimitReader(resp.Body, webFetchMaxBytes)
	body, err := io.ReadAll(limited)
	if err != nil {
		return "", fmt.Errorf("failed to read DDG response: %w", err)
	}

	results := ParseDDGHTML(string(body), query)
	return results, nil
}

// ParseDDGHTML extracts result titles, URLs, and snippets from DDG's HTML response
// without any external HTML-parsing dependency.
func ParseDDGHTML(html, query string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Web search results for: %q\n", query))
	sb.WriteString("Source: DuckDuckGo\n\n")

	// DDG wraps each result in <div class="result__body"> or similar.
	// We extract title+url from <a class="result__a" href="...">title</a>
	// and snippet from <a class="result__snippet">...</a>

	reResult := regexp.MustCompile(`(?is)<a[^>]+class="result__a"[^>]+href="([^"]+)"[^>]*>(.*?)</a>`)
	reSnippet := regexp.MustCompile(`(?is)<a[^>]+class="result__snippet"[^>]*>(.*?)</a>`)

	titleMatches := reResult.FindAllStringSubmatch(html, webSearchMaxResults*2)
	snippetMatches := reSnippet.FindAllStringSubmatch(html, webSearchMaxResults*2)

	count := 0
	for i, m := range titleMatches {
		if count >= webSearchMaxResults {
			break
		}
		rawURL := m[1]
		title := strings.TrimSpace(StripHTML(m[2]))
		if title == "" || rawURL == "" {
			continue
		}

		// DDG sometimes wraps URLs in redirects like //duckduckgo.com/l/?...uddg=<real_url>
		// Try to extract the real URL from the uddg param
		if strings.Contains(rawURL, "duckduckgo.com/l/") || strings.HasPrefix(rawURL, "//") {
			if parsed, err := url.Parse(rawURL); err == nil {
				if uddg := parsed.Query().Get("uddg"); uddg != "" {
					if decoded, err := url.QueryUnescape(uddg); err == nil {
						rawURL = decoded
					}
				}
			}
		}

		count++
		sb.WriteString(fmt.Sprintf("\n%d. %s\n", count, title))
		sb.WriteString(fmt.Sprintf("   URL: %s\n", rawURL))

		if i < len(snippetMatches) {
			snippet := strings.TrimSpace(StripHTML(snippetMatches[i][1]))
			if len([]rune(snippet)) > 300 {
				snippet = string([]rune(snippet)[:300]) + "..."
			}
			if snippet != "" {
				sb.WriteString(fmt.Sprintf("   %s\n", snippet))
			}
		}
	}

	if count == 0 {
		sb.WriteString("No results could be parsed from DuckDuckGo. Try a different query or use web_fetch directly.\n")
	} else {
		sb.WriteString("\nTip: Use web_fetch with any URL above to read the full page content.")
	}

	return sb.String()
}

// --- registerWebTools wires both tools into the registry ---

func (r *Registry) registerWebTools() {
	// web_fetch
	r.RegisterTool(providers.ToolDefinition{
		Type: "function",
		Function: struct {
			Name        string                 `json:"name"`
			Description string                 `json:"description"`
			Parameters  map[string]interface{} `json:"parameters"`
		}{
			Name:        "web_fetch",
			Description: "Fetches the content of any public URL and returns clean, readable text. Use this to read articles, documentation, API responses, or any web page the user mentions. Automatically strips HTML tags.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"url": map[string]interface{}{
						"type":        "string",
						"description": "The full URL to fetch (must start with http:// or https://).",
					},
				},
				"required": []string{"url"},
			},
		},
	}, func(ctx context.Context, args map[string]interface{}) *ToolResult {
		rawURL, ok := args["url"].(string)
		if !ok || rawURL == "" {
			return &ToolResult{ForLLM: "Error: url must be a non-empty string"}
		}
		if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
			return &ToolResult{ForLLM: "Error: url must start with http:// or https://"}
		}

		text, err := DoWebFetch(rawURL)
		if err != nil {
			return &ToolResult{ForLLM: fmt.Sprintf("web_fetch failed: %v", err)}
		}
		return &ToolResult{ForLLM: text}
	})

	// web_search — Tavily primary, DuckDuckGo fallback
	searchDesc := "Searches the web for up-to-date information on any topic and returns a list of results with titles, URLs, and summaries."
	if r.tavilyAPIKey != "" {
		searchDesc += " Uses Tavily as the primary search engine with DuckDuckGo as automatic fallback."
	} else {
		searchDesc += " (Tavily not configured — using DuckDuckGo. Add a Tavily API key via 'littleclaw configure' for higher quality results.)"
	}

	r.RegisterTool(providers.ToolDefinition{
		Type: "function",
		Function: struct {
			Name        string                 `json:"name"`
			Description string                 `json:"description"`
			Parameters  map[string]interface{} `json:"parameters"`
		}{
			Name:        "web_search",
			Description: searchDesc,
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "The search query. Be specific and descriptive for best results.",
					},
				},
				"required": []string{"query"},
			},
		},
	}, func(ctx context.Context, args map[string]interface{}) *ToolResult {
		query, ok := args["query"].(string)
		if !ok || query == "" {
			return &ToolResult{ForLLM: "Error: query must be a non-empty string"}
		}

		// Try Tavily first if key is available
		if r.tavilyAPIKey != "" {
			result, rateLimited, err := tavilySearch(ctx, r.tavilyAPIKey, query)
			if err == nil {
				return &ToolResult{ForLLM: result}
			}

			// On rate limit or any error, fall through to DuckDuckGo
			fallbackReason := "error"
			if rateLimited {
				fallbackReason = "rate limit reached"
			}
			fmt.Printf("⚠️  Tavily search failed (%s: %v), falling back to DuckDuckGo\n", fallbackReason, err)
		}

		// DuckDuckGo fallback
		result, err := duckDuckGoSearch(ctx, query)
		if err != nil {
			return &ToolResult{ForLLM: fmt.Sprintf("Both Tavily and DuckDuckGo searches failed. Last error: %v", err)}
		}
		return &ToolResult{ForLLM: result}
	})
}
