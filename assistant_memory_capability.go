package main

import (
	"fmt"
	"strings"
	"time"
)

type MemoryCapability struct {
	Config AssistantConfig
}

func NewMemoryCapability(cfg AssistantConfig) *MemoryCapability {
	return &MemoryCapability{Config: cfg}
}

func (c *MemoryCapability) Name() string { return "memory" }

func (c *MemoryCapability) Description() string {
	return "Store, search, inspect, and update personal memory inferred from user context."
}

func (c *MemoryCapability) Tools() []Tool {
	return []Tool{
		{Name: "memory.remember", Description: "Store an observation or inferred fact in memory.", ParamSchema: `{"type":"object","properties":{"kind":{"type":"string"},"bucket":{"type":"string"},"subject":{"type":"string"},"contact":{"type":"string"},"key":{"type":"string"},"summary":{"type":"string"},"value":{"type":"string"},"evidence":{"type":"string"},"source_type":{"type":"string"},"source_id":{"type":"string"},"inference_reason":{"type":"string"},"importance":{"type":"integer"},"confidence":{"type":"string"},"verification":{"type":"string"},"effective_start":{"type":"string"},"effective_end":{"type":"string"},"inferred":{"type":"boolean"}}}`},
		{Name: "memory.search", Description: "Search memory semantically for relevant facts.", ParamSchema: `{"type":"object","properties":{"query":{"type":"string"},"limit":{"type":"integer"}},"required":["query"]}`},
		{Name: "memory.recall", Description: "Recall the most relevant memory items for the current user request.", ParamSchema: `{"type":"object","properties":{"query":{"type":"string"},"limit":{"type":"integer"}},"required":["query"]}`},
		{Name: "memory.inspect", Description: "Inspect stored memory facts, contacts, and recent observations.", ParamSchema: `{"type":"object","properties":{"limit":{"type":"integer"}}}`},
		{Name: "memory.update", Description: "Update a stored memory fact by id.", ParamSchema: `{"type":"object","properties":{"id":{"type":"string"},"summary":{"type":"string"},"value":{"type":"string"},"kind":{"type":"string"},"bucket":{"type":"string"},"importance":{"type":"integer"},"effective_start":{"type":"string"},"effective_end":{"type":"string"}},"required":["id"]}`},
	}
}

func (c *MemoryCapability) Execute(toolName string, params map[string]any) (ToolResult, error) {
	mem, err := LoadAssistantMemory(c.Config)
	if err != nil {
		return ToolResult{Success: false, Error: err.Error()}, err
	}
	switch strings.ToLower(strings.TrimSpace(toolName)) {
	case "memory.remember":
		return c.executeRemember(mem, params)
	case "memory.search":
		return c.executeSearch(mem, params, false)
	case "memory.recall":
		return c.executeSearch(mem, params, true)
	case "memory.inspect":
		limit := assistantClampIntParam(params, 8, 1, 20)
		facts := mem.BestFacts()
		observations := mem.Observations()
		inferred := mem.Inferences()
		return ToolResult{Success: true, Text: fmt.Sprintf("Loaded %d memory facts.", len(mem.BestFacts())), Data: map[string]any{
			"facts":        facts[:memoryCapabilityMin(limit, len(facts))],
			"contacts":     mem.Contacts(),
			"observations": observations[:memoryCapabilityMin(limit, len(observations))],
			"inferred":     inferred[:memoryCapabilityMin(limit, len(inferred))],
		}}, nil
	case "memory.update":
		id := strings.TrimSpace(firstStringParam(params, "id"))
		if id == "" {
			err := fmt.Errorf("id is required")
			return ToolResult{Success: false, Error: err.Error()}, err
		}
		fact, ok, err := mem.UpdateFact(id, MemoryFact{
			Summary:        strings.TrimSpace(firstStringParam(params, "summary")),
			Value:          strings.TrimSpace(firstStringParam(params, "value")),
			Kind:           MemoryKind(strings.TrimSpace(firstStringParam(params, "kind"))),
			Bucket:         MemoryBucket(strings.TrimSpace(firstStringParam(params, "bucket"))),
			Importance:     assistantIntValue(params["importance"]),
			EffectiveStart: assistantParseMemoryTime(firstStringParam(params, "effective_start", "effectiveStart")),
			EffectiveEnd:   assistantParseMemoryTime(firstStringParam(params, "effective_end", "effectiveEnd")),
		})
		if err != nil {
			return ToolResult{Success: false, Error: err.Error()}, err
		}
		if !ok {
			err := fmt.Errorf("memory fact %q was not found", id)
			return ToolResult{Success: false, Error: err.Error()}, err
		}
		return ToolResult{Success: true, Text: "Memory updated.", Data: fact}, nil
	default:
		err := fmt.Errorf("unknown memory tool %q", toolName)
		return ToolResult{Success: false, Error: err.Error()}, err
	}
}

func (c *MemoryCapability) executeRemember(mem *AssistantMemory, params map[string]any) (ToolResult, error) {
	if paramBool(params, "inferred") {
		item, err := mem.AddInference(MemoryInference{
			Kind:            MemoryKind(strings.TrimSpace(firstStringParam(params, "kind"))),
			Bucket:          MemoryBucket(strings.TrimSpace(firstStringParam(params, "bucket"))),
			Subject:         strings.TrimSpace(firstStringParam(params, "subject")),
			ContactAlias:    strings.TrimSpace(firstStringParam(params, "contact")),
			Key:             strings.TrimSpace(firstStringParam(params, "key")),
			Summary:         strings.TrimSpace(firstStringParam(params, "summary")),
			Value:           strings.TrimSpace(firstStringParam(params, "value")),
			Evidence:        strings.TrimSpace(firstStringParam(params, "evidence")),
			SourceType:      strings.TrimSpace(firstStringParam(params, "source_type", "sourceType")),
			SourceID:        strings.TrimSpace(firstStringParam(params, "source_id", "sourceId")),
			InferenceReason: strings.TrimSpace(firstStringParam(params, "inference_reason", "inferenceReason")),
			Importance:      assistantIntValue(params["importance"]),
			Confidence:      MemoryConfidence(strings.TrimSpace(firstStringParam(params, "confidence"))),
			Verification:    MemoryVerification(strings.TrimSpace(firstStringParam(params, "verification"))),
			EffectiveStart:  assistantParseMemoryTime(firstStringParam(params, "effective_start", "effectiveStart")),
			EffectiveEnd:    assistantParseMemoryTime(firstStringParam(params, "effective_end", "effectiveEnd")),
		})
		if err != nil {
			return ToolResult{Success: false, Error: err.Error()}, err
		}
		return ToolResult{Success: true, Text: "Inferred memory stored.", Data: item.toFact()}, nil
	}
	item, err := mem.AddObservation(MemoryObservation{
		Kind:          MemoryKind(strings.TrimSpace(firstStringParam(params, "kind"))),
		Bucket:        MemoryBucket(strings.TrimSpace(firstStringParam(params, "bucket"))),
		Subject:       strings.TrimSpace(firstStringParam(params, "subject")),
		ContactAlias:  strings.TrimSpace(firstStringParam(params, "contact")),
		Key:           strings.TrimSpace(firstStringParam(params, "key")),
		Summary:       strings.TrimSpace(firstStringParam(params, "summary")),
		Value:         strings.TrimSpace(firstStringParam(params, "value")),
		Evidence:      strings.TrimSpace(firstStringParam(params, "evidence")),
		SourceType:    strings.TrimSpace(firstStringParam(params, "source_type", "sourceType")),
		SourceID:      strings.TrimSpace(firstStringParam(params, "source_id", "sourceId")),
		Importance:    assistantIntValue(params["importance"]),
		Confidence:    MemoryConfidence(strings.TrimSpace(firstStringParam(params, "confidence"))),
		Verification:  MemoryVerification(strings.TrimSpace(firstStringParam(params, "verification"))),
		EffectiveStart: assistantParseMemoryTime(firstStringParam(params, "effective_start", "effectiveStart")),
		EffectiveEnd:   assistantParseMemoryTime(firstStringParam(params, "effective_end", "effectiveEnd")),
	})
	if err != nil {
		return ToolResult{Success: false, Error: err.Error()}, err
	}
	return ToolResult{Success: true, Text: "Observation stored.", Data: memoryFactFromObservation(item)}, nil
}

func (c *MemoryCapability) executeSearch(mem *AssistantMemory, params map[string]any, recall bool) (ToolResult, error) {
	query := strings.TrimSpace(firstStringParam(params, "query", "q", "input"))
	if query == "" {
		err := fmt.Errorf("query is required")
		return ToolResult{Success: false, Error: err.Error()}, err
	}
	limit := assistantClampIntParam(params, 6, 1, 12)
	var items []MemoryFact
	if recall {
		items = mem.Recall(query, limit)
	} else {
		searchResults := mem.SearchResults(query, limit)
		items = make([]MemoryFact, 0, len(searchResults))
		for _, result := range searchResults {
			if result.Fact.Key != "" || result.Fact.Summary != "" || result.Fact.Value != "" {
				items = append(items, result.Fact)
				continue
			}
			fact := memoryFactFromObservation(result.Observation)
			if fact.Key == "" && fact.Summary == "" && fact.Value == "" {
				continue
			}
			items = append(items, fact)
		}
	}
	text := fmt.Sprintf("Found %d relevant memory item(s).", len(items))
	data := map[string]any{
		"query": query,
		"items": items,
	}
	if !recall {
		data["results"] = mem.SearchResults(query, limit)
	}
	return ToolResult{Success: true, Text: text, Data: data}, nil
}

func assistantParseMemoryTime(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC()
		}
	}
	return time.Time{}
}

func memoryCapabilityMin(a, b int) int {
	if a < b {
		return a
	}
	return b
}
