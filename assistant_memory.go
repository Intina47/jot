package main

import (
	"encoding/json"
	"errors"
	"os"
	"sort"
	"strings"
	"time"
)

const assistantMemoryFileName = "assistant_memory.json"

type MemoryConfidence string
type MemoryVerification string

const (
	MemoryConfidenceHigh    MemoryConfidence = "high"
	MemoryConfidenceMedium  MemoryConfidence = "medium"
	MemoryConfidenceLow     MemoryConfidence = "low"
	MemoryConfidenceUnknown MemoryConfidence = "unknown"

	MemoryVerificationUserConfirmed MemoryVerification = "user_confirmed"
	MemoryVerificationToolVerified  MemoryVerification = "tool_verified"
	MemoryVerificationVerified      MemoryVerification = "verified"
	MemoryVerificationInferred      MemoryVerification = "inferred"
)

type AssistantMemory struct {
	path             string
	Owner            string                      `json:"owner,omitempty"`
	ContactsByID     map[string]AssistantContact `json:"contacts,omitempty"`
	ObservationItems []MemoryObservation         `json:"observations,omitempty"`
	UpdatedAt        time.Time                   `json:"updatedAt,omitempty"`
}

type AssistantContact struct {
	ID      string   `json:"id"`
	Label   string   `json:"label,omitempty"`
	Aliases []string `json:"aliases,omitempty"`
}

type MemoryObservation struct {
	ID           string             `json:"id"`
	Scope        string             `json:"scope,omitempty"`
	ContactID    string             `json:"contactId,omitempty"`
	ContactAlias string             `json:"contactAlias,omitempty"`
	Key          string             `json:"key"`
	Value        string             `json:"value"`
	Evidence     string             `json:"evidence,omitempty"`
	SourceType   string             `json:"sourceType"`
	SourceID     string             `json:"sourceId,omitempty"`
	ObservedAt   time.Time          `json:"observedAt"`
	EffectiveAt  time.Time          `json:"effectiveAt,omitempty"`
	Confidence   MemoryConfidence   `json:"confidence,omitempty"`
	Verification MemoryVerification `json:"verification,omitempty"`
}

type MemoryFact struct {
	ContactID    string             `json:"contactId,omitempty"`
	Key          string             `json:"key"`
	Value        string             `json:"value"`
	SourceType   string             `json:"sourceType,omitempty"`
	SourceID     string             `json:"sourceId,omitempty"`
	ObservedAt   time.Time          `json:"observedAt,omitempty"`
	Confidence   MemoryConfidence   `json:"confidence,omitempty"`
	Verification MemoryVerification `json:"verification,omitempty"`
}

func NewAssistantMemoryAt(path string) *AssistantMemory {
	memory := &AssistantMemory{path: strings.TrimSpace(path)}
	memory.normalize()
	return memory
}

func LoadAssistantMemory(cfg AssistantConfig) (*AssistantMemory, error) {
	return LoadAssistantMemoryAt(cfg.MemoryPath)
}

func LoadAssistantMemoryAt(path string) (*AssistantMemory, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return NewAssistantMemoryAt(""), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return NewAssistantMemoryAt(path), nil
		}
		return nil, err
	}
	memory := NewAssistantMemoryAt(path)
	if err := json.Unmarshal(trimUTF8BOM(data), memory); err != nil {
		return nil, err
	}
	memory.path = path
	memory.normalize()
	return memory, nil
}

func (m *AssistantMemory) Save(path string) error {
	if m == nil {
		return nil
	}
	if strings.TrimSpace(path) == "" {
		path = m.path
	}
	m.normalize()
	m.path = strings.TrimSpace(path)
	return writeSecureJSON(path, m)
}

func (m *AssistantMemory) AddObservation(observation MemoryObservation) (MemoryObservation, error) {
	if m == nil {
		return MemoryObservation{}, nil
	}
	m.normalize()
	observation = m.normalizeObservation(observation)
	m.ObservationItems = append(m.ObservationItems, observation)
	m.UpdatedAt = time.Now().UTC()
	if strings.TrimSpace(m.path) != "" {
		if err := m.Save(m.path); err != nil {
			return MemoryObservation{}, err
		}
	}
	return observation, nil
}

func (m *AssistantMemory) LinkContactAlias(contactID, alias string) (string, error) {
	if m == nil {
		return "", nil
	}
	m.normalize()
	contactID = strings.TrimSpace(contactID)
	alias = strings.TrimSpace(alias)
	if contactID == "" || alias == "" {
		return "", nil
	}
	contact := m.ContactsByID[contactID]
	contact.ID = contactID
	contact.Aliases = assistantNormalizeStringList(append(contact.Aliases, alias))
	if contact.Label == "" {
		contact.Label = alias
	}
	m.ContactsByID[contactID] = contact
	m.UpdatedAt = time.Now().UTC()
	if strings.TrimSpace(m.path) != "" {
		if err := m.Save(m.path); err != nil {
			return "", err
		}
	}
	return contactID, nil
}

func (m *AssistantMemory) ResolveContactID(alias string) (string, bool) {
	id := m.ResolveContact(alias)
	return id, id != ""
}

func (m *AssistantMemory) ResolveContact(alias string) string {
	if m == nil {
		return ""
	}
	alias = strings.TrimSpace(alias)
	if alias == "" {
		return ""
	}
	if _, ok := m.ContactsByID[alias]; ok {
		return alias
	}
	lower := strings.ToLower(alias)
	for id, contact := range m.ContactsByID {
		if strings.EqualFold(contact.Label, alias) {
			return id
		}
		for _, candidate := range contact.Aliases {
			if strings.ToLower(candidate) == lower {
				return id
			}
		}
	}
	return ""
}

func (m *AssistantMemory) Observations() []MemoryObservation {
	if m == nil || len(m.ObservationItems) == 0 {
		return nil
	}
	out := make([]MemoryObservation, len(m.ObservationItems))
	copy(out, m.ObservationItems)
	return out
}

func (m *AssistantMemory) Contacts() []AssistantContact {
	if m == nil || len(m.ContactsByID) == 0 {
		return nil
	}
	out := make([]AssistantContact, 0, len(m.ContactsByID))
	for _, contact := range m.ContactsByID {
		out = append(out, contact)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (m *AssistantMemory) BestFacts() []MemoryFact {
	if m == nil {
		return nil
	}
	type factKey struct {
		scope   string
		contact string
		name    string
	}
	best := map[factKey]MemoryFact{}
	for _, item := range m.ObservationItems {
		if strings.TrimSpace(item.Key) == "" || strings.TrimSpace(item.Value) == "" {
			continue
		}
		k := factKey{
			scope:   strings.TrimSpace(item.Scope),
			contact: strings.TrimSpace(item.ContactID),
			name:    strings.TrimSpace(item.Key),
		}
		next := MemoryFact{
			ContactID:    strings.TrimSpace(item.ContactID),
			Key:          strings.TrimSpace(item.Key),
			Value:        strings.TrimSpace(item.Value),
			SourceType:   strings.TrimSpace(item.SourceType),
			SourceID:     strings.TrimSpace(item.SourceID),
			ObservedAt:   item.ObservedAt,
			Confidence:   item.Confidence,
			Verification: item.Verification,
		}
		current, ok := best[k]
		if !ok || memoryObservationRank(next) > memoryObservationRank(current) {
			best[k] = next
		}
	}
	out := make([]MemoryFact, 0, len(best))
	for _, item := range best {
		out = append(out, item)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].ContactID == out[j].ContactID {
			return out[i].Key < out[j].Key
		}
		return out[i].ContactID < out[j].ContactID
	})
	return out
}

func (m *AssistantMemory) FactsByKey(key string) []MemoryFact {
	key = strings.TrimSpace(key)
	if key == "" || m == nil {
		return nil
	}
	var out []MemoryFact
	for _, item := range m.BestFacts() {
		if item.Key == key {
			out = append(out, item)
		}
	}
	return out
}

func (m *AssistantMemory) BestFact(key string) (MemoryFact, bool) {
	key = strings.TrimSpace(key)
	if key == "" || m == nil {
		return MemoryFact{}, false
	}
	for _, item := range m.BestFacts() {
		if item.Key == key {
			return item, true
		}
	}
	return MemoryFact{}, false
}

func (m *AssistantMemory) normalize() {
	if m.ContactsByID == nil {
		m.ContactsByID = make(map[string]AssistantContact)
	}
	for id, contact := range m.ContactsByID {
		contact.ID = assistantDefaultString(contact.ID, id)
		contact.Label = strings.TrimSpace(contact.Label)
		contact.Aliases = assistantNormalizeStringList(contact.Aliases)
		m.ContactsByID[id] = contact
	}
}

func (m *AssistantMemory) normalizeObservation(observation MemoryObservation) MemoryObservation {
	observation.Scope = strings.TrimSpace(strings.ToLower(observation.Scope))
	observation.ContactID = strings.TrimSpace(observation.ContactID)
	observation.ContactAlias = strings.TrimSpace(observation.ContactAlias)
	observation.Key = strings.TrimSpace(observation.Key)
	observation.Value = strings.TrimSpace(observation.Value)
	observation.Evidence = strings.TrimSpace(observation.Evidence)
	observation.SourceType = strings.TrimSpace(observation.SourceType)
	observation.SourceID = strings.TrimSpace(observation.SourceID)
	if observation.ObservedAt.IsZero() {
		observation.ObservedAt = time.Now().UTC()
	}
	if observation.ContactID == "" && observation.ContactAlias != "" {
		if resolved, ok := m.ResolveContactID(observation.ContactAlias); ok {
			observation.ContactID = resolved
		} else {
			observation.ContactID = "contact-" + strings.ToLower(strings.ReplaceAll(observation.ContactAlias, " ", "-"))
		}
	}
	if observation.ContactID != "" && observation.ContactAlias != "" {
		_, _ = m.LinkContactAlias(observation.ContactID, observation.ContactAlias)
	}
	observation.ID = strings.TrimSpace(observation.ID)
	if observation.ID == "" {
		observation.ID = memoryObservationID(observation)
	}
	return observation
}

func memoryObservationID(observation MemoryObservation) string {
	parts := []string{
		strings.TrimSpace(observation.Scope),
		strings.TrimSpace(observation.ContactID),
		strings.TrimSpace(observation.Key),
		strings.TrimSpace(observation.Value),
		strings.TrimSpace(observation.SourceType),
		strings.TrimSpace(observation.SourceID),
		observation.ObservedAt.UTC().Format(time.RFC3339Nano),
	}
	return strings.Join(parts, "|")
}

func memoryObservationRank(item MemoryFact) int {
	score := 0
	switch strings.ToLower(strings.TrimSpace(string(item.Verification))) {
	case string(MemoryVerificationUserConfirmed):
		score += 400
	case string(MemoryVerificationVerified):
		score += 300
	case string(MemoryVerificationToolVerified):
		score += 250
	case string(MemoryVerificationInferred):
		score += 100
	}
	switch strings.ToLower(strings.TrimSpace(string(item.Confidence))) {
	case string(MemoryConfidenceHigh):
		score += 40
	case string(MemoryConfidenceMedium):
		score += 20
	case string(MemoryConfidenceLow):
		score += 10
	}
	switch strings.ToLower(strings.TrimSpace(item.SourceType)) {
	case "user":
		score += 50
	case "gmail", "calendar", "browser", "form":
		score += 25
	}
	score += int(item.ObservedAt.UTC().Unix() / 3600)
	return score
}
