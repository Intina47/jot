package main

import (
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

type AssistantWebResult struct {
	Title       string    `json:"title,omitempty"`
	URL         string    `json:"url,omitempty"`
	Snippet     string    `json:"snippet,omitempty"`
	Source      string    `json:"source,omitempty"`
	PublishedAt time.Time `json:"publishedAt,omitempty"`
}

type AssistantWebPage struct {
	Title   string `json:"title,omitempty"`
	URL     string `json:"url,omitempty"`
	Text    string `json:"text,omitempty"`
	Summary string `json:"summary,omitempty"`
}

type WebCapability struct {
	BaseURL   string
	Client    *http.Client
	UserAgent string
}

func NewWebCapability(_ ...AssistantConfig) *WebCapability {
	return &WebCapability{
		BaseURL:   "https://www.bing.com",
		Client:    &http.Client{Timeout: 15 * time.Second},
		UserAgent: "jot-assistant/1.0",
	}
}

func (c *WebCapability) Name() string { return "web" }

func (c *WebCapability) Description() string {
	return "Search the public web and fetch public pages for research-backed assistant work."
}

func (c *WebCapability) Tools() []Tool {
	return []Tool{
		{
			Name:        "web.search",
			Description: "Search the web for current public information.",
			ParamSchema: `{"type":"object","properties":{"query":{"type":"string"},"limit":{"type":"integer"}},"required":["query"]}`,
		},
		{
			Name:        "web.fetch",
			Description: "Fetch a public web page and extract readable text for reasoning.",
			ParamSchema: `{"type":"object","properties":{"url":{"type":"string"},"max_chars":{"type":"integer"}},"required":["url"]}`,
		},
	}
}

func (c *WebCapability) Execute(toolName string, params map[string]any) (ToolResult, error) {
	switch strings.ToLower(strings.TrimSpace(toolName)) {
	case "web.search":
		query := strings.TrimSpace(firstStringParam(params, "query", "q", "input"))
		if query == "" {
			err := fmt.Errorf("query is required")
			return ToolResult{Success: false, Error: err.Error()}, err
		}
		limit := assistantClampIntParam(params, 5, 1, 8)
		if maxResults := assistantIntValue(params["max_results"]); maxResults > 0 {
			limit = maxResults
		}
		results, err := c.search(query, limit)
		if err != nil {
			return ToolResult{Success: false, Error: err.Error()}, err
		}
		rows := make([]map[string]any, 0, len(results))
		for _, result := range results {
			rows = append(rows, map[string]any{
				"title":       result.Title,
				"url":         result.URL,
				"snippet":     result.Snippet,
				"source":      assistantDefaultString(result.Source, "Bing RSS"),
				"publishedAt": result.PublishedAt.Format(time.RFC3339),
			})
		}
		return ToolResult{
			Success: true,
			Text:    fmt.Sprintf("Top results for %q are ready.", query),
			Data: map[string]any{
				"query":   query,
				"source":  "Bing RSS",
				"results": rows,
			},
		}, nil
	case "web.fetch":
		pageURL := strings.TrimSpace(firstStringParam(params, "url"))
		if pageURL == "" {
			err := fmt.Errorf("url is required")
			return ToolResult{Success: false, Error: err.Error()}, err
		}
		maxChars := assistantClampIntParam(params, 4000, 800, 12000)
		page, statusCode, err := c.fetch(pageURL, maxChars)
		if err != nil {
			return ToolResult{Success: false, Error: err.Error()}, err
		}
		return ToolResult{
			Success: true,
			Text:    "Fetched web page.",
			Data: map[string]any{
				"title":      page.Title,
				"url":        page.URL,
				"text":       page.Text,
				"summary":    page.Summary,
				"sourceType": "web_page",
				"statusCode": statusCode,
			},
		}, nil
	default:
		err := fmt.Errorf("unknown web tool %q", toolName)
		return ToolResult{Success: false, Error: err.Error()}, err
	}
}

func (c *WebCapability) search(query string, limit int) ([]AssistantWebResult, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	if baseURL == "" {
		baseURL = "https://www.bing.com"
	}
	client := c.Client
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	userAgent := strings.TrimSpace(c.UserAgent)
	if userAgent == "" {
		userAgent = "jot-assistant/1.0"
	}
	return assistantWebSearch(baseURL, client, userAgent, query, limit)
}

func (c *WebCapability) fetch(pageURL string, maxChars int) (AssistantWebPage, int, error) {
	client := c.Client
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	userAgent := strings.TrimSpace(c.UserAgent)
	if userAgent == "" {
		userAgent = "jot-assistant/1.0"
	}
	return assistantWebFetch(client, userAgent, pageURL, maxChars)
}

type assistantBingRSS struct {
	Channel struct {
		Items []struct {
			Title       string `xml:"title"`
			Link        string `xml:"link"`
			Description string `xml:"description"`
			PubDate     string `xml:"pubDate"`
			Source      string `xml:"source"`
		} `xml:"item"`
	} `xml:"channel"`
}

func assistantWebSearch(baseURL string, client *http.Client, userAgent, query string, limit int) ([]AssistantWebResult, error) {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = "https://www.bing.com"
	}
	if strings.TrimSpace(userAgent) == "" {
		userAgent = "jot-assistant/1.0"
	}
	endpoint := baseURL + "/search?format=rss&q=" + url.QueryEscape(strings.TrimSpace(query))
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("web search failed: %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	var feed assistantBingRSS
	if err := xml.Unmarshal(body, &feed); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 5
	}
	results := make([]AssistantWebResult, 0, limit)
	for _, item := range feed.Channel.Items {
		result := AssistantWebResult{
			Title:   strings.TrimSpace(html.UnescapeString(item.Title)),
			URL:     strings.TrimSpace(item.Link),
			Snippet: assistantCleanHTML(item.Description),
			Source:  assistantDefaultString(strings.TrimSpace(item.Source), "Bing RSS"),
		}
		if parsed, err := time.Parse(time.RFC1123Z, strings.TrimSpace(item.PubDate)); err == nil {
			result.PublishedAt = parsed.UTC()
		}
		if result.Title == "" || result.URL == "" {
			continue
		}
		results = append(results, result)
		if len(results) >= limit {
			break
		}
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("web search returned no results")
	}
	return results, nil
}

var (
	assistantHTMLTagPattern    = regexp.MustCompile(`(?s)<[^>]+>`)
	assistantHTMLTitlePattern  = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	assistantHTMLScriptPattern = regexp.MustCompile(`(?is)<(script|style)[^>]*>.*?</(script|style)>`)
)

func assistantWebFetch(client *http.Client, userAgent, pageURL string, maxChars int) (AssistantWebPage, int, error) {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	if strings.TrimSpace(userAgent) == "" {
		userAgent = "jot-assistant/1.0"
	}
	req, err := http.NewRequest(http.MethodGet, strings.TrimSpace(pageURL), nil)
	if err != nil {
		return AssistantWebPage{}, 0, err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil {
		return AssistantWebPage{}, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return AssistantWebPage{}, resp.StatusCode, fmt.Errorf("web fetch failed: %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return AssistantWebPage{}, resp.StatusCode, err
	}
	raw := string(body)
	page := AssistantWebPage{URL: strings.TrimSpace(pageURL)}
	if match := assistantHTMLTitlePattern.FindStringSubmatch(raw); len(match) > 1 {
		page.Title = assistantCleanHTML(match[1])
	}
	raw = assistantHTMLScriptPattern.ReplaceAllString(raw, " ")
	page.Text = assistantCleanHTML(raw)
	if maxChars <= 0 {
		maxChars = 4000
	}
	if len(page.Text) > maxChars {
		page.Text = strings.TrimSpace(page.Text[:maxChars]) + "..."
	}
	page.Summary = assistantWebSummary(page)
	return page, resp.StatusCode, nil
}

func assistantCleanHTML(value string) string {
	value = assistantHTMLTagPattern.ReplaceAllString(value, " ")
	value = html.UnescapeString(value)
	value = strings.Join(strings.Fields(value), " ")
	return strings.TrimSpace(value)
}

func assistantWebSummary(page AssistantWebPage) string {
	title := strings.TrimSpace(page.Title)
	text := strings.TrimSpace(page.Text)
	if text == "" {
		return title
	}
	if len(text) > 240 {
		text = strings.TrimSpace(text[:240]) + "..."
	}
	if title == "" {
		return text
	}
	if text == "" {
		return title
	}
	return title + " - " + text
}
