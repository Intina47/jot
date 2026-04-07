package main

import (
	"path/filepath"
	"testing"
)

func TestMemoryCapability_RememberInferredAndRecall(t *testing.T) {
	cfg := AssistantConfig{MemoryPath: filepath.Join(t.TempDir(), assistantMemoryFileName)}
	cap := NewMemoryCapability(cfg)

	result, err := cap.Execute("memory.remember", map[string]any{
		"inferred":         true,
		"kind":             "situation",
		"bucket":           "tentative",
		"subject":          "Ntina",
		"key":              "army training graduation",
		"summary":          "Ntina will likely graduate basic training in late June",
		"value":            "late June graduation",
		"evidence":         "basic training timeline mentioned in chat",
		"inference_reason": "basic training usually lasts several weeks",
		"confidence":       "medium",
		"verification":     "inferred",
	})
	if err != nil {
		t.Fatalf("memory.remember returned error: %v", err)
	}
	if !result.Success {
		t.Fatalf("memory.remember was not successful: %#v", result)
	}

	mem, err := LoadAssistantMemory(cfg)
	if err != nil {
		t.Fatalf("LoadAssistantMemory returned error: %v", err)
	}
	if got := len(mem.Inferences()); got != 1 {
		t.Fatalf("expected 1 inference, got %d", got)
	}

	search, err := cap.Execute("memory.recall", map[string]any{"query": "when will Ntina graduate basic training"})
	if err != nil {
		t.Fatalf("memory.recall returned error: %v", err)
	}
	data, ok := search.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected map data, got %#v", search.Data)
	}
	items, ok := data["items"].([]MemoryFact)
	if !ok {
		t.Fatalf("expected []MemoryFact items, got %#v", data["items"])
	}
	if len(items) == 0 {
		t.Fatalf("expected recalled items, got %#v", items)
	}
	if items[0].InferenceReason == "" {
		t.Fatalf("expected recalled fact to preserve inference reason, got %#v", items[0])
	}
}

func TestMemoryCapability_SearchIncludesSemanticResults(t *testing.T) {
	cfg := AssistantConfig{MemoryPath: filepath.Join(t.TempDir(), assistantMemoryFileName)}
	mem := NewAssistantMemoryAt(cfg.MemoryPath)
	if _, err := mem.AddObservation(MemoryObservation{
		Scope:      "user",
		Subject:    "Ntina",
		Key:        "current project",
		Summary:    "working on Jot memory",
		Value:      "building Jot memory inference",
		Evidence:   "journal note about memory architecture",
		SourceType: "journal",
	}); err != nil {
		t.Fatalf("AddObservation returned error: %v", err)
	}

	cap := NewMemoryCapability(cfg)
	result, err := cap.Execute("memory.search", map[string]any{"query": "what is Ntina building right now"})
	if err != nil {
		t.Fatalf("memory.search returned error: %v", err)
	}
	data, ok := result.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected map data, got %#v", result.Data)
	}
	items, ok := data["items"].([]MemoryFact)
	if !ok {
		t.Fatalf("expected []MemoryFact items, got %#v", data["items"])
	}
	if len(items) == 0 {
		t.Fatalf("expected semantic search results, got %#v", data)
	}
	if items[0].Key != "current project" {
		t.Fatalf("expected project fact to rank first, got %#v", items[0])
	}
}
