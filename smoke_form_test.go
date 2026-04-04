package main

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestSmokeMicrosoftFormNoSubmit(t *testing.T) {
	rawURL := strings.TrimSpace(os.Getenv("JOT_SMOKE_FORM_URL"))
	if rawURL == "" {
		t.Skip("JOT_SMOKE_FORM_URL is not set")
	}

	provider, err := NewModelProvider(mustLoadSmokeAssistantConfig(t))
	if err != nil {
		t.Fatalf("NewModelProvider: %v", err)
	}

	snapshot, fields, model, attempts, err := observeMicrosoftFormNoSubmit(t, rawURL, provider)
	if err != nil {
		for _, line := range attempts {
			t.Log(line)
		}
		t.Fatalf("observeMicrosoftFormNoSubmit: %v", err)
	}
	for _, line := range attempts {
		t.Log(line)
	}
	t.Logf("title=%q url=%q fields=%d required=%d/%d next=%v submit=%v vision=%v conf=%q", snapshot.Title, snapshot.URL, len(fields), model.RequiredAnswered, model.RequiredTotal, model.NextAvailable, model.SubmitAvailable, model.VisionUsed, model.VisionConfidence)
	t.Logf("visible-text=%q", assistantTruncateText(snapshot.Text, 1200))
	for i, field := range fields {
		t.Logf("field[%d]: label=%q type=%q required=%v options=%v", i, field.Label, field.Type, field.Required, field.Options)
	}
	if len(fields) <= 2 {
		for i, el := range snapshot.Elements {
			if i >= 40 {
				break
			}
			t.Logf("element[%d]: role=%q label=%q group=%q context=%q value=%q checked=%v selector=%q", i, el.Role, el.Label, el.GroupLabel, assistantTruncateText(el.Context, 160), el.Value, el.Checked, el.Selector)
		}
	}

	if len(fields) == 0 {
		t.Fatalf("expected at least one visible form field after hydration attempts")
	}
	if len(model.RequiredUnanswered) > 0 {
		t.Logf("required unanswered: %s", strings.Join(model.RequiredUnanswered, ", "))
	}
}

func mustLoadSmokeAssistantConfig(t *testing.T) AssistantConfig {
	t.Helper()
	cfg, err := LoadAssistantConfig(AssistantConfigOverrides{})
	if err != nil {
		t.Fatalf("LoadAssistantConfig: %v", err)
	}
	return cfg
}

func observeMicrosoftFormNoSubmit(t *testing.T, rawURL string, provider ModelProvider) (BrowserPageSnapshot, []FormField, BrowserFormPageModel, []string, error) {
	t.Helper()
	browser, err := NewBrowserComputer(BrowserComputerOptions{
		StartURL: rawURL,
		Headless: true,
		KeepOpen: false,
	})
	if err != nil {
		return BrowserPageSnapshot{}, nil, BrowserFormPageModel{}, nil, err
	}
	defer browser.Close()

	attempts := make([]string, 0, 6)
	var snapshot BrowserPageSnapshot
	var fields []FormField
	var model BrowserFormPageModel
	var lastErr error
	for i := 0; i < 6; i++ {
		_, snap, err := browserPerceptionForFill(browser)
		if err != nil {
			lastErr = err
			attempts = append(attempts, fmt.Sprintf("attempt %d: perception error: %v", i+1, err))
			time.Sleep(900 * time.Millisecond)
			continue
		}
		snapshot = snap
		fields = browserFormFieldsFromSnapshot(snapshot)
		model = buildBrowserFormPageModelWithVision(provider, browser, snapshot, nil)
		attempts = append(attempts, fmt.Sprintf(
			"attempt %d: title=%q fields=%d required=%d/%d unanswered=%v next=%v submit=%v vision=%v conf=%q",
			i+1,
			snapshot.Title,
			len(fields),
			model.RequiredAnswered,
			model.RequiredTotal,
			model.RequiredUnanswered,
			model.NextAvailable,
			model.SubmitAvailable,
			model.VisionUsed,
			model.VisionConfidence,
		))
		if len(fields) > 1 || model.NextAvailable || model.SubmitAvailable || len(model.RequiredUnanswered) > 0 {
			return snapshot, fields, model, attempts, nil
		}
		time.Sleep(900 * time.Millisecond)
	}
	if lastErr != nil {
		return snapshot, fields, model, attempts, lastErr
	}
	return snapshot, fields, model, attempts, nil
}
