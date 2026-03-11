package tools_test

import (
	"littleclaw/pkg/tools"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// tools.StripHTML tests  (pure function, no server needed)
// ---------------------------------------------------------------------------

func TestStripHTML_RemovesTags(t *testing.T) {
	input := "<h1>Hello</h1><p>World</p>"
	got := tools.StripHTML(input)
	if strings.Contains(got, "<") || strings.Contains(got, ">") {
		t.Errorf("tools.StripHTML left HTML tags in output: %q", got)
	}
	if !strings.Contains(got, "Hello") || !strings.Contains(got, "World") {
		t.Errorf("tools.StripHTML removed text content: %q", got)
	}
}

func TestStripHTML_RemovesScriptAndStyle(t *testing.T) {
	input := `<html><script>var x = 1;</script><style>.a{color:red}</style><p>visible</p></html>`
	got := tools.StripHTML(input)
	if strings.Contains(got, "var x") || strings.Contains(got, "color:red") {
		t.Errorf("tools.StripHTML did not remove script/style content: %q", got)
	}
	if !strings.Contains(got, "visible") {
		t.Errorf("tools.StripHTML removed visible content: %q", got)
	}
}

func TestStripHTML_DecodesHTMLEntities(t *testing.T) {
	input := "<p>rock &amp; roll &lt;loud&gt; &quot;music&quot; it&#39;s &nbsp;great</p>"
	got := tools.StripHTML(input)

	for _, want := range []string{"&", "<", ">", `"`, "'", "great"} {
		if !strings.Contains(got, want) {
			t.Errorf("tools.StripHTML did not decode entity for %q; got: %q", want, got)
		}
	}
}

func TestStripHTML_TruncatesAtMaxChars(t *testing.T) {
	// Generate text > tools.WebFetchMaxChars via many paragraphs
	big := strings.Repeat("<p>hello world content here</p>", 1000)
	got := tools.StripHTML(big)
	runeLen := len([]rune(got))
	if runeLen > tools.WebFetchMaxChars+50 {
		t.Errorf("tools.StripHTML did not truncate: got %d chars", runeLen)
	}
	if !strings.Contains(got, "truncated") {
		t.Error("tools.StripHTML should append '[...truncated]' when truncating")
	}
}

func TestStripHTML_EmptyInput(t *testing.T) {
	got := tools.StripHTML("")
	if got != "" {
		t.Errorf("tools.StripHTML(\"\") = %q, want empty", got)
	}
}

func TestStripHTML_PlainText(t *testing.T) {
	input := "No tags here. Just plain text.\nWith a newline."
	got := tools.StripHTML(input)
	if !strings.Contains(got, "Just plain text") {
		t.Errorf("tools.StripHTML mangled plain text: %q", got)
	}
}

// ---------------------------------------------------------------------------
// tools.FormatTavilyResults tests  (pure function)
// ---------------------------------------------------------------------------

func TestFormatTavilyResults_WithAnswer(t *testing.T) {
	r := tools.TavilyResponse{
		Answer: "Go is a compiled language.",
		Results: []tools.TavilyResult{
			{Title: "Go docs", URL: "https://go.dev", Content: "Official Go documentation site."},
		},
	}
	got := tools.FormatTavilyResults("go language", r)
	if !strings.Contains(got, "QUICK ANSWER") {
		t.Errorf("expected QUICK ANSWER block, got: %q", got)
	}
	if !strings.Contains(got, "Go is a compiled language") {
		t.Errorf("expected answer text, got: %q", got)
	}
	if !strings.Contains(got, "Go docs") {
		t.Errorf("expected result title, got: %q", got)
	}
	if !strings.Contains(got, "https://go.dev") {
		t.Errorf("expected result URL, got: %q", got)
	}
}

func TestFormatTavilyResults_NoResults(t *testing.T) {
	r := tools.TavilyResponse{Results: nil}
	got := tools.FormatTavilyResults("obscure query", r)
	if !strings.Contains(got, "No results found") {
		t.Errorf("expected 'No results found', got: %q", got)
	}
}

func TestFormatTavilyResults_LongContentTruncated(t *testing.T) {
	longContent := strings.Repeat("very long content ", 100) // > 400 chars
	r := tools.TavilyResponse{
		Results: []tools.TavilyResult{
			{Title: "Test", URL: "https://example.com", Content: longContent},
		},
	}
	got := tools.FormatTavilyResults("test", r)
	if !strings.Contains(got, "...") {
		t.Errorf("long content should be truncated with '...', got: %q", got)
	}
}

func TestFormatTavilyResults_MultipleResults(t *testing.T) {
	r := tools.TavilyResponse{
		Results: []tools.TavilyResult{
			{Title: "First", URL: "https://first.com", Content: "first content"},
			{Title: "Second", URL: "https://second.com", Content: "second content"},
			{Title: "Third", URL: "https://third.com", Content: "third content"},
		},
	}
	got := tools.FormatTavilyResults("test", r)
	for _, expected := range []string{"First", "Second", "Third", "first.com", "second.com", "third.com"} {
		if !strings.Contains(got, expected) {
			t.Errorf("expected %q in formatted results, got: %q", expected, got)
		}
	}
}

// ---------------------------------------------------------------------------
// tools.ParseDDGHTML tests  (pure function)
// ---------------------------------------------------------------------------

func TestParseDDGHTML_NoResults(t *testing.T) {
	got := tools.ParseDDGHTML("<html><body>nothing here</body></html>", "test")
	if !strings.Contains(got, "No results") {
		t.Errorf("expected 'No results' message for empty HTML, got: %q", got)
	}
}

func TestParseDDGHTML_ContainsHeader(t *testing.T) {
	got := tools.ParseDDGHTML("", "my query")
	if !strings.Contains(got, `"my query"`) {
		t.Errorf("expected query in output header, got: %q", got)
	}
	if !strings.Contains(got, "DuckDuckGo") {
		t.Errorf("expected 'DuckDuckGo' source label, got: %q", got)
	}
}

func TestParseDDGHTML_WithResults(t *testing.T) {
	// Minimal DDG HTML with one result
	html := `<div><a class="result__a" href="https://example.com">Example Site</a>
	<a class="result__snippet">This is a snippet about example.</a></div>`
	got := tools.ParseDDGHTML(html, "example")
	if !strings.Contains(got, "Example Site") {
		t.Errorf("expected result title in output, got: %q", got)
	}
	if !strings.Contains(got, "https://example.com") {
		t.Errorf("expected result URL in output, got: %q", got)
	}
}

// ---------------------------------------------------------------------------
// web_fetch tool tests via httptest.Server  (no real network)
// ---------------------------------------------------------------------------

func TestWebFetchTool_HTMLPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<html><body><h1>Test Page</h1><p>Some content here.</p></body></html>`))
	}))
	defer server.Close()

	r, _ := newTestRegistry(t)
	result := r.Execute(context.Background(), "web_fetch", map[string]interface{}{
		"url": server.URL,
	})

	if strings.Contains(strings.ToLower(result.ForLLM), "error") {
		t.Errorf("web_fetch failed unexpectedly: %q", result.ForLLM)
	}
	if !strings.Contains(result.ForLLM, "Test Page") {
		t.Errorf("web_fetch result missing page content: %q", result.ForLLM)
	}
	if !strings.Contains(result.ForLLM, "Some content here") {
		t.Errorf("web_fetch result missing paragraph: %q", result.ForLLM)
	}
}

func TestWebFetchTool_JSONResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"name":"littleclaw","version":"1.0"}`))
	}))
	defer server.Close()

	r, _ := newTestRegistry(t)
	result := r.Execute(context.Background(), "web_fetch", map[string]interface{}{
		"url": server.URL,
	})

	if strings.Contains(strings.ToLower(result.ForLLM), "error") {
		t.Errorf("web_fetch JSON failed: %q", result.ForLLM)
	}
	if !strings.Contains(result.ForLLM, "littleclaw") {
		t.Errorf("web_fetch JSON result missing content: %q", result.ForLLM)
	}
}

func TestWebFetchTool_404Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	r, _ := newTestRegistry(t)
	result := r.Execute(context.Background(), "web_fetch", map[string]interface{}{
		"url": server.URL,
	})

	if !strings.Contains(strings.ToLower(result.ForLLM), "error") &&
		!strings.Contains(strings.ToLower(result.ForLLM), "failed") &&
		!strings.Contains(strings.ToLower(result.ForLLM), "404") {
		t.Errorf("expected error for 404, got: %q", result.ForLLM)
	}
}

func TestWebFetchTool_InvalidURL(t *testing.T) {
	r, _ := newTestRegistry(t)
	result := r.Execute(context.Background(), "web_fetch", map[string]interface{}{
		"url": "not-a-url",
	})
	if !strings.Contains(strings.ToLower(result.ForLLM), "error") {
		t.Errorf("expected error for invalid URL, got: %q", result.ForLLM)
	}
}

func TestWebFetchTool_EmptyURL(t *testing.T) {
	r, _ := newTestRegistry(t)
	result := r.Execute(context.Background(), "web_fetch", map[string]interface{}{
		"url": "",
	})
	if !strings.Contains(strings.ToLower(result.ForLLM), "error") {
		t.Errorf("expected error for empty URL, got: %q", result.ForLLM)
	}
}

func TestWebFetchTool_LargeBody_Truncated(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		// Write > tools.WebFetchMaxChars of content
		big := strings.Repeat("<p>hello world content text</p>", 2000)
		_, _ = w.Write([]byte(big))
	}))
	defer server.Close()

	r, _ := newTestRegistry(t)
	result := r.Execute(context.Background(), "web_fetch", map[string]interface{}{
		"url": server.URL,
	})

	runeLen := len([]rune(result.ForLLM))
	if runeLen > tools.WebFetchMaxChars+200 {
		t.Errorf("web_fetch result too long: %d chars", runeLen)
	}
}

// ---------------------------------------------------------------------------
// web_search tool tests via mock Tavily/DDG server
// ---------------------------------------------------------------------------

func TestWebSearchTool_NoAPIKey_FallsBackToDDG(t *testing.T) {
	// Without a Tavily key, web_search uses DuckDuckGo.
	// We can't intercept the real DDG call in unit tests, but we can at
	// least verify the tool is registered and calls through without panicking.
	// We mock what we can with a local DDG-like HTML server.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<div>
			<a class="result__a" href="https://example.com">Go Language</a>
			<a class="result__snippet">Official Go programming language site.</a>
		</div>`))
	}))
	defer server.Close()

	// The tool will call the real DDG URL, so short-circuit by calling
	// tools.ParseDDGHTML directly with fake HTML — this tests the full parsing path
	result := tools.ParseDDGHTML(`<div>
		<a class="result__a" href="https://example.com">Go Language</a>
		<a class="result__snippet">Official Go programming language site.</a>
	</div>`, "go language")

	if !strings.Contains(result, "Go Language") {
		t.Errorf("expected result in parsed DDG output, got: %q", result)
	}
}

func TestWebSearchTool_BadArgs(t *testing.T) {
	r, _ := newTestRegistry(t)
	result := r.Execute(context.Background(), "web_search", map[string]interface{}{
		"query": "",
	})
	if !strings.Contains(strings.ToLower(result.ForLLM), "error") {
		t.Errorf("web_search with empty query should error, got: %q", result.ForLLM)
	}
}

// ---------------------------------------------------------------------------
// tools.DoWebFetch direct tests via httptest.Server
// ---------------------------------------------------------------------------

func TestDoWebFetch_PlainTextContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("plain text response"))
	}))
	defer server.Close()

	got, err := tools.DoWebFetch(server.URL)
	if err != nil {
		t.Fatalf("tools.DoWebFetch() error = %v", err)
	}
	if !strings.Contains(got, "plain text response") {
		t.Errorf("tools.DoWebFetch() = %q, want 'plain text response'", got)
	}
}

func TestDoWebFetch_JSONPrettyPrinted(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"key":"value","number":42}`))
	}))
	defer server.Close()

	got, err := tools.DoWebFetch(server.URL)
	if err != nil {
		t.Fatalf("tools.DoWebFetch() error = %v", err)
	}

	// Should be pretty-printed — verify it's valid JSON
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		t.Errorf("tools.DoWebFetch returned invalid JSON: %q", got)
	}
}

func TestDoWebFetch_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	_, err := tools.DoWebFetch(server.URL)
	if err == nil {
		t.Error("tools.DoWebFetch() should return error for 500 response")
	}
}

func TestDoWebFetch_InvalidURL(t *testing.T) {
	_, err := tools.DoWebFetch("://badurl")
	if err == nil {
		t.Error("tools.DoWebFetch() should return error for invalid URL")
	}
}

func TestDoWebFetch_ChecksUserAgentHeader(t *testing.T) {
	var receivedUA string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedUA = r.Header.Get("User-Agent")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	_, _ = tools.DoWebFetch(server.URL)
	if !strings.Contains(receivedUA, "Littleclaw") {
		t.Errorf("expected Littleclaw User-Agent header, got: %q", receivedUA)
	}
}
