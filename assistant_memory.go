package main

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"sort"
	"strings"
	"time"
)

const assistantMemoryFileName = "assistant_memory.json"
const assistantMemorySchemaVersion = 2

type MemoryConfidence string
type MemoryVerification string
type MemoryBucket string
type MemoryKind string

const (
	MemoryConfidenceHigh    MemoryConfidence = "high"
	MemoryConfidenceMedium  MemoryConfidence = "medium"
	MemoryConfidenceLow     MemoryConfidence = "low"
	MemoryConfidenceUnknown MemoryConfidence = "unknown"

	MemoryVerificationUserConfirmed MemoryVerification = "user_confirmed"
	MemoryVerificationToolVerified  MemoryVerification = "tool_verified"
	MemoryVerificationVerified      MemoryVerification = "verified"
	MemoryVerificationInferred      MemoryVerification = "inferred"

	MemoryBucketDurable   MemoryBucket = "durable"
	MemoryBucketActive    MemoryBucket = "active"
	MemoryBucketScheduled MemoryBucket = "scheduled"
	MemoryBucketTentative MemoryBucket = "tentative"
	MemoryBucketExpired   MemoryBucket = "expired"

	MemoryKindFact         MemoryKind = "fact"
	MemoryKindProfile      MemoryKind = "profile"
	MemoryKindSituation    MemoryKind = "situation"
	MemoryKindProject      MemoryKind = "project"
	MemoryKindPreference   MemoryKind = "preference"
	MemoryKindEpisode      MemoryKind = "episode"
	MemoryKindRelationship MemoryKind = "relationship"
	MemoryKindNote         MemoryKind = "note"
)

type AssistantMemory struct {
	path string

	SchemaVersion    int                         `json:"schemaVersion,omitempty"`
	Owner            string                      `json:"owner,omitempty"`
	ContactsByID     map[string]AssistantContact `json:"contactsByID,omitempty"`
	ObservationItems []MemoryObservation         `json:"observationItems,omitempty"`
	InferenceItems   []MemoryInference           `json:"inferences,omitempty"`
	FactItems        []MemoryFact                `json:"factItems,omitempty"`
	UpdatedAt        time.Time                   `json:"updatedAt,omitempty"`
}

type AssistantContact struct {
	ID      string   `json:"id"`
	Label   string   `json:"label,omitempty"`
	Aliases []string `json:"aliases,omitempty"`
}

type MemoryObservation struct {
	ID                   string             `json:"id"`
	Scope                string             `json:"scope,omitempty"`
	Subject              string             `json:"subject,omitempty"`
	ContactID            string             `json:"contactId,omitempty"`
	ContactAlias         string             `json:"contactAlias,omitempty"`
	Key                  string             `json:"key"`
	Summary              string             `json:"summary,omitempty"`
	Value                string             `json:"value"`
	Evidence             string             `json:"evidence,omitempty"`
	EvidenceRefs         []string           `json:"evidenceRefs,omitempty"`
	SourceType           string             `json:"sourceType"`
	SourceID             string             `json:"sourceId,omitempty"`
	SourceObservationIDs []string           `json:"sourceObservationIds,omitempty"`
	ObservedAt           time.Time          `json:"observedAt"`
	EffectiveAt          time.Time          `json:"effectiveAt,omitempty"`
	EffectiveStart       time.Time          `json:"effectiveStart,omitempty"`
	EffectiveEnd         time.Time          `json:"effectiveEnd,omitempty"`
	Confidence           MemoryConfidence   `json:"confidence,omitempty"`
	Verification         MemoryVerification `json:"verification,omitempty"`
	Kind                 MemoryKind         `json:"kind,omitempty"`
	Bucket               MemoryBucket       `json:"bucket,omitempty"`
	Importance           int                `json:"importance,omitempty"`
	RetrievalText        string             `json:"retrievalText,omitempty"`
	InferenceReason      string             `json:"inferenceReason,omitempty"`
}

type MemoryFact struct {
	ID                   string             `json:"id,omitempty"`
	Scope                string             `json:"scope,omitempty"`
	Subject              string             `json:"subject,omitempty"`
	ContactID            string             `json:"contactId,omitempty"`
	ContactAlias         string             `json:"contactAlias,omitempty"`
	Key                  string             `json:"key"`
	Summary              string             `json:"summary,omitempty"`
	Value                string             `json:"value"`
	Evidence             string             `json:"evidence,omitempty"`
	EvidenceRefs         []string           `json:"evidenceRefs,omitempty"`
	SourceType           string             `json:"sourceType,omitempty"`
	SourceID             string             `json:"sourceId,omitempty"`
	SourceObservationIDs []string           `json:"sourceObservationIds,omitempty"`
	ObservedAt           time.Time          `json:"observedAt,omitempty"`
	EffectiveAt          time.Time          `json:"effectiveAt,omitempty"`
	EffectiveStart       time.Time          `json:"effectiveStart,omitempty"`
	EffectiveEnd         time.Time          `json:"effectiveEnd,omitempty"`
	Confidence           MemoryConfidence   `json:"confidence,omitempty"`
	Verification         MemoryVerification `json:"verification,omitempty"`
	Kind                 MemoryKind         `json:"kind,omitempty"`
	Bucket               MemoryBucket       `json:"bucket,omitempty"`
	Importance           int                `json:"importance,omitempty"`
	LastUsedAt           time.Time          `json:"lastUsedAt,omitempty"`
	RetrievalText        string             `json:"retrievalText,omitempty"`
	InferenceReason      string             `json:"inferenceReason,omitempty"`
}

type MemorySearchResult struct {
	Score       float64           `json:"score,omitempty"`
	Kind        MemoryKind        `json:"kind,omitempty"`
	Bucket      MemoryBucket      `json:"bucket,omitempty"`
	Text        string            `json:"text,omitempty"`
	Fact        MemoryFact        `json:"fact,omitempty"`
	Observation MemoryObservation `json:"observation,omitempty"`
}

type MemoryInference struct {
	ID              string             `json:"id,omitempty"`
	Kind            MemoryKind         `json:"kind,omitempty"`
	Bucket          MemoryBucket       `json:"bucket,omitempty"`
	Scope           string             `json:"scope,omitempty"`
	Subject         string             `json:"subject,omitempty"`
	ContactID       string             `json:"contactId,omitempty"`
	ContactAlias    string             `json:"contactAlias,omitempty"`
	Key             string             `json:"key,omitempty"`
	Summary         string             `json:"summary,omitempty"`
	Value           string             `json:"value,omitempty"`
	Evidence        string             `json:"evidence,omitempty"`
	EvidenceRefs    []string           `json:"evidenceRefs,omitempty"`
	SourceType      string             `json:"sourceType,omitempty"`
	SourceID        string             `json:"sourceId,omitempty"`
	ObservedAt      time.Time          `json:"observedAt,omitempty"`
	EffectiveStart  time.Time          `json:"effectiveStart,omitempty"`
	EffectiveEnd    time.Time          `json:"effectiveEnd,omitempty"`
	Confidence      MemoryConfidence   `json:"confidence,omitempty"`
	Verification    MemoryVerification `json:"verification,omitempty"`
	Importance      int                `json:"importance,omitempty"`
	InferenceReason string             `json:"inferenceReason,omitempty"`
	RetrievalText   string             `json:"retrievalText,omitempty"`
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
	memory := &AssistantMemory{path: path}
	if err := json.Unmarshal(trimUTF8BOM(data), memory); err != nil {
		return nil, err
	}
	originalSchemaVersion := memory.SchemaVersion
	memory.normalize()
	if originalSchemaVersion < assistantMemorySchemaVersion {
		if err := memory.Save(path); err != nil {
			return nil, err
		}
	}
	return memory, nil
}

func (m *AssistantMemory) Save(path string) error {
	if m == nil {
		return nil
	}
	if strings.TrimSpace(path) == "" {
		path = m.path
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	m.normalize()
	m.path = path
	return writeSecureJSON(path, m)
}

func (m *AssistantMemory) AddObservation(observation MemoryObservation) (MemoryObservation, error) {
	if m == nil {
		return MemoryObservation{}, nil
	}
	m.normalize()
	observation = m.normalizeObservation(observation)
	if stored, handled := m.upsertObservationLifecycle(observation); handled {
		m.normalize()
		m.UpdatedAt = time.Now().UTC()
		if strings.TrimSpace(m.path) != "" {
			if err := m.Save(m.path); err != nil {
				return MemoryObservation{}, err
			}
		}
		return stored, nil
	}
	m.ObservationItems = append(m.ObservationItems, observation)
	m.normalize()
	m.UpdatedAt = time.Now().UTC()
	if strings.TrimSpace(m.path) != "" {
		if err := m.Save(m.path); err != nil {
			return MemoryObservation{}, err
		}
	}
	stored, ok := m.observationByID(observation.ID)
	if ok {
		return stored, nil
	}
	return observation, nil
}

func (m *AssistantMemory) AddFact(fact MemoryFact) (MemoryFact, error) {
	if m == nil {
		return MemoryFact{}, nil
	}
	m.normalize()
	fact = m.normalizeFact(fact)
	if stored, handled := m.upsertFactLifecycle(fact); handled {
		m.normalize()
		m.UpdatedAt = time.Now().UTC()
		if strings.TrimSpace(m.path) != "" {
			if err := m.Save(m.path); err != nil {
				return MemoryFact{}, err
			}
		}
		return stored, nil
	}
	m.FactItems = append(m.FactItems, fact)
	m.normalize()
	m.UpdatedAt = time.Now().UTC()
	if strings.TrimSpace(m.path) != "" {
		if err := m.Save(m.path); err != nil {
			return MemoryFact{}, err
		}
	}
	stored, ok := m.factByID(fact.ID)
	if ok {
		return stored, nil
	}
	return fact, nil
}

func (m *AssistantMemory) AddInference(inference MemoryInference) (MemoryInference, error) {
	if m == nil {
		return MemoryInference{}, nil
	}
	m.normalize()
	inference = m.normalizeInference(inference)
	if stored, handled := m.upsertInferenceLifecycle(inference); handled {
		m.normalize()
		m.UpdatedAt = time.Now().UTC()
		if strings.TrimSpace(m.path) != "" {
			if err := m.Save(m.path); err != nil {
				return MemoryInference{}, err
			}
		}
		return stored, nil
	}
	m.InferenceItems = append(m.InferenceItems, inference)
	m.normalize()
	m.UpdatedAt = time.Now().UTC()
	if strings.TrimSpace(m.path) != "" {
		if err := m.Save(m.path); err != nil {
			return MemoryInference{}, err
		}
	}
	stored, ok := m.inferenceByID(inference.ID)
	if ok {
		return stored, nil
	}
	return inference, nil
}

func (m *AssistantMemory) upsertObservationLifecycle(next MemoryObservation) (MemoryObservation, bool) {
	for idx, existing := range m.ObservationItems {
		if (memoryObservationLifecycleMatch(existing, next) && memoryObservationEquivalentValue(existing, next)) || memoryObservationSemanticEquivalent(existing, next) {
			merged := memoryMergeObservation(existing, next)
			merged = m.normalizeObservation(merged)
			m.ObservationItems[idx] = merged
			return merged, true
		}
		if memoryObservationShouldExpire(existing, next) && memoryObservationComparable(existing, next) {
			m.ObservationItems[idx] = m.normalizeObservation(memoryExpireObservation(existing, next.ObservedAt))
		}
	}
	return MemoryObservation{}, false
}

func (m *AssistantMemory) upsertInferenceLifecycle(next MemoryInference) (MemoryInference, bool) {
	for idx, existing := range m.InferenceItems {
		if (memoryInferenceLifecycleMatch(existing, next) && memoryInferenceEquivalentValue(existing, next)) || memoryInferenceSemanticEquivalent(existing, next) {
			merged := memoryMergeInference(existing, next)
			merged = m.normalizeInference(merged)
			m.InferenceItems[idx] = merged
			return merged, true
		}
		if memoryInferenceShouldExpire(existing, next) && memoryInferenceComparable(existing, next) {
			m.InferenceItems[idx] = m.normalizeInference(memoryExpireInference(existing, next.ObservedAt))
		}
	}
	return MemoryInference{}, false
}

func (m *AssistantMemory) upsertFactLifecycle(next MemoryFact) (MemoryFact, bool) {
	for idx, existing := range m.FactItems {
		if ((memoryFactLifecycleMatch(existing, next) && memoryFactEquivalentValue(existing, next)) || memoryFactSemanticEquivalent(existing, next)) && !memoryFactReplacementConflict(existing, next) {
			merged := memoryMergeFact(existing, next)
			merged = m.normalizeFact(merged)
			m.FactItems[idx] = merged
			return merged, true
		}
		if memoryFactShouldExpire(existing, next) && memoryFactComparable(existing, next) {
			m.FactItems[idx] = m.normalizeFact(memoryExpireFact(existing, next.ObservedAt))
		}
	}
	return MemoryFact{}, false
}

func (m *AssistantMemory) Inferences() []MemoryInference {
	if m == nil || len(m.InferenceItems) == 0 {
		return nil
	}
	m.normalize()
	out := make([]MemoryInference, len(m.InferenceItems))
	copy(out, m.InferenceItems)
	sort.SliceStable(out, func(i, j int) bool {
		ri := memoryFactRank(out[i].toFact())
		rj := memoryFactRank(out[j].toFact())
		if ri == rj {
			if out[i].ObservedAt.Equal(out[j].ObservedAt) {
				return out[i].ID < out[j].ID
			}
			return out[i].ObservedAt.After(out[j].ObservedAt)
		}
		return ri > rj
	})
	return out
}

func (m *AssistantMemory) LinkContactAlias(contactID, alias string) (string, error) {
	if m == nil {
		return "", nil
	}
	m.normalize()
	contactID = strings.TrimSpace(contactID)
	alias = strings.TrimSpace(alias)
	if alias == "" {
		return "", nil
	}
	if contactID == "" {
		contactID = memoryContactIDFromAlias(alias)
	}
	m.upsertContactAlias(contactID, alias)
	m.UpdatedAt = time.Now().UTC()
	if strings.TrimSpace(m.path) != "" {
		if err := m.Save(m.path); err != nil {
			return "", err
		}
	}
	return contactID, nil
}

func (m *AssistantMemory) UpdateFact(id string, patch MemoryFact) (MemoryFact, bool, error) {
	if m == nil {
		return MemoryFact{}, false, nil
	}
	m.normalize()
	id = strings.TrimSpace(id)
	if id == "" {
		return MemoryFact{}, false, nil
	}
	for idx, fact := range m.FactItems {
		if fact.ID != id {
			continue
		}
		if strings.TrimSpace(patch.Scope) != "" {
			fact.Scope = strings.TrimSpace(patch.Scope)
		}
		if strings.TrimSpace(patch.Subject) != "" {
			fact.Subject = strings.TrimSpace(patch.Subject)
		}
		if strings.TrimSpace(patch.ContactID) != "" {
			fact.ContactID = strings.TrimSpace(patch.ContactID)
		}
		if strings.TrimSpace(patch.ContactAlias) != "" {
			fact.ContactAlias = strings.TrimSpace(patch.ContactAlias)
		}
		if strings.TrimSpace(patch.Key) != "" {
			fact.Key = strings.TrimSpace(patch.Key)
		}
		if strings.TrimSpace(patch.Summary) != "" {
			fact.Summary = strings.TrimSpace(patch.Summary)
			if fact.Value == "" {
				fact.Value = fact.Summary
			}
		}
		if strings.TrimSpace(patch.Value) != "" {
			fact.Value = strings.TrimSpace(patch.Value)
			if fact.Summary == "" {
				fact.Summary = fact.Value
			}
		}
		if strings.TrimSpace(patch.Evidence) != "" {
			fact.Evidence = strings.TrimSpace(patch.Evidence)
		}
		if len(patch.EvidenceRefs) > 0 {
			fact.EvidenceRefs = assistantNormalizeStringList(patch.EvidenceRefs)
		}
		if strings.TrimSpace(patch.SourceType) != "" {
			fact.SourceType = strings.ToLower(strings.TrimSpace(patch.SourceType))
		}
		if strings.TrimSpace(patch.SourceID) != "" {
			fact.SourceID = strings.TrimSpace(patch.SourceID)
		}
		if len(patch.SourceObservationIDs) > 0 {
			fact.SourceObservationIDs = assistantNormalizeStringList(patch.SourceObservationIDs)
		}
		if !patch.ObservedAt.IsZero() {
			fact.ObservedAt = patch.ObservedAt.UTC()
		}
		if !patch.EffectiveStart.IsZero() {
			fact.EffectiveStart = patch.EffectiveStart.UTC()
		}
		if !patch.EffectiveEnd.IsZero() {
			fact.EffectiveEnd = patch.EffectiveEnd.UTC()
		}
		if !patch.EffectiveAt.IsZero() {
			fact.EffectiveAt = patch.EffectiveAt.UTC()
		}
		if strings.TrimSpace(string(patch.Confidence)) != "" {
			fact.Confidence = normalizeConfidence(patch.Confidence)
		}
		if strings.TrimSpace(string(patch.Verification)) != "" {
			fact.Verification = normalizeVerification(patch.Verification)
		}
		if strings.TrimSpace(string(patch.Kind)) != "" {
			fact.Kind = normalizeKind(patch.Kind)
		}
		if strings.TrimSpace(string(patch.Bucket)) != "" {
			fact.Bucket = normalizeBucket(patch.Bucket)
		}
		if patch.Importance != 0 {
			fact.Importance = clampInt(patch.Importance, 1, 100)
		}
		if strings.TrimSpace(patch.InferenceReason) != "" {
			fact.InferenceReason = strings.TrimSpace(patch.InferenceReason)
		}
		if strings.TrimSpace(patch.RetrievalText) != "" {
			fact.RetrievalText = strings.TrimSpace(patch.RetrievalText)
		}
		fact = m.normalizeFact(fact)
		m.FactItems[idx] = fact
		m.normalize()
		if strings.TrimSpace(m.path) != "" {
			if err := m.Save(m.path); err != nil {
				return MemoryFact{}, false, err
			}
		}
		return fact, true, nil
	}
	return MemoryFact{}, false, nil
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
	m.normalize()
	if _, ok := m.ContactsByID[alias]; ok {
		return alias
	}
	return m.resolveContactIDUnlocked(alias)
}

func (m *AssistantMemory) Observations() []MemoryObservation {
	if m == nil || len(m.ObservationItems) == 0 {
		return nil
	}
	m.normalize()
	out := make([]MemoryObservation, len(m.ObservationItems))
	copy(out, m.ObservationItems)
	return out
}

func (m *AssistantMemory) Contacts() []AssistantContact {
	if m == nil || len(m.ContactsByID) == 0 {
		return nil
	}
	m.normalize()
	out := make([]AssistantContact, 0, len(m.ContactsByID))
	for _, contact := range m.ContactsByID {
		out = append(out, contact)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Label == out[j].Label {
			return out[i].ID < out[j].ID
		}
		return out[i].Label < out[j].Label
	})
	return out
}

func (m *AssistantMemory) Facts() []MemoryFact {
	if m == nil || len(m.FactItems) == 0 {
		return nil
	}
	m.normalize()
	out := make([]MemoryFact, len(m.FactItems))
	copy(out, m.FactItems)
	return out
}

func (m *AssistantMemory) BestFacts() []MemoryFact {
	return m.Facts()
}

func (m *AssistantMemory) FactsByKey(key string) []MemoryFact {
	key = strings.TrimSpace(key)
	if key == "" || m == nil {
		return nil
	}
	m.normalize()
	var out []MemoryFact
	for _, item := range m.FactItems {
		if normalizeMemoryText(item.Key) == normalizeMemoryText(key) {
			out = append(out, item)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		ri := memoryFactRank(out[i])
		rj := memoryFactRank(out[j])
		if ri == rj {
			if out[i].ObservedAt.Equal(out[j].ObservedAt) {
				return out[i].Value < out[j].Value
			}
			return out[i].ObservedAt.After(out[j].ObservedAt)
		}
		return ri > rj
	})
	return out
}

func (m *AssistantMemory) BestFact(key string) (MemoryFact, bool) {
	key = strings.TrimSpace(key)
	if key == "" || m == nil {
		return MemoryFact{}, false
	}
	facts := m.FactsByKey(key)
	if len(facts) == 0 {
		return MemoryFact{}, false
	}
	return facts[0], true
}

func (m *AssistantMemory) SearchResults(query string, limit int) []MemorySearchResult {
	if m == nil {
		return nil
	}
	m.normalize()
	query = strings.TrimSpace(query)
	if limit <= 0 {
		limit = 10
	}
	results := make([]MemorySearchResult, 0, len(m.FactItems)+len(m.ObservationItems))
	for _, fact := range m.FactItems {
		text := memorySearchTextFromFact(fact)
		semantic := memorySemanticScore(query, text)
		if semantic <= 0 {
			continue
		}
		score := semantic*1000 + float64(memoryFactRank(fact))
		results = append(results, MemorySearchResult{
			Score:  score,
			Kind:   fact.Kind,
			Bucket: fact.Bucket,
			Text:   text,
			Fact:   fact,
		})
	}
	for _, obs := range m.ObservationItems {
		text := memorySearchTextFromObservation(obs)
		semantic := memorySemanticScore(query, text)
		if semantic <= 0 {
			continue
		}
		score := semantic*1000 + float64(memoryFactRank(memoryFactFromObservation(obs)))
		results = append(results, MemorySearchResult{
			Score:       score,
			Kind:        obs.Kind,
			Bucket:      obs.Bucket,
			Text:        text,
			Observation: obs,
		})
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Score == results[j].Score {
			if results[i].Text == results[j].Text {
				return results[i].Fact.ID < results[j].Fact.ID
			}
			return results[i].Text < results[j].Text
		}
		return results[i].Score > results[j].Score
	})
	if limit > len(results) {
		limit = len(results)
	}
	return results[:limit]
}

func (m *AssistantMemory) Search(query string, limit int) []MemorySearchResult {
	return m.SearchResults(query, limit)
}

func (m *AssistantMemory) SearchFacts(query string, limit int) []MemoryFact {
	results := m.SearchResults(query, limit)
	out := make([]MemoryFact, 0, len(results))
	seen := map[string]struct{}{}
	for _, result := range results {
		fact := result.Fact
		if fact.Key == "" {
			fact = memoryFactFromObservation(result.Observation)
		}
		if fact.Key == "" {
			continue
		}
		id := memoryFactIdentity(fact)
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, fact)
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (m *AssistantMemory) Recall(query string, limit int) []MemoryFact {
	return m.SearchFacts(query, limit)
}

func (m *AssistantMemory) normalize() {
	if m == nil {
		return
	}
	if m.SchemaVersion < assistantMemorySchemaVersion {
		m.SchemaVersion = assistantMemorySchemaVersion
	}
	if m.ContactsByID == nil {
		m.ContactsByID = make(map[string]AssistantContact)
	}

	normalizedContacts := make(map[string]AssistantContact, len(m.ContactsByID))
	for id, contact := range m.ContactsByID {
		contact = normalizeContact(id, contact)
		if contact.ID == "" {
			continue
		}
		if existing, ok := normalizedContacts[contact.ID]; ok {
			normalizedContacts[contact.ID] = mergeContacts(existing, contact)
			continue
		}
		normalizedContacts[contact.ID] = contact
	}
	m.ContactsByID = normalizedContacts

	normalizedObservations := make(map[string]MemoryObservation, len(m.ObservationItems))
	for _, observation := range m.ObservationItems {
		observation = m.normalizeObservation(observation)
		if observation.Key == "" && observation.Value == "" {
			continue
		}
		if existing, ok := normalizedObservations[observation.ID]; ok {
			if memoryObservationRank(observation) >= memoryObservationRank(existing) {
				normalizedObservations[observation.ID] = observation
			}
			continue
		}
		normalizedObservations[observation.ID] = observation
	}
	m.ObservationItems = mapObservations(normalizedObservations)

	normalizedInferences := make(map[string]MemoryInference, len(m.InferenceItems))
	for _, inference := range m.InferenceItems {
		inference = m.normalizeInference(inference)
		if inference.Key == "" && inference.Value == "" && inference.Summary == "" {
			continue
		}
		if existing, ok := normalizedInferences[inference.ID]; ok {
			if memoryFactRank(inference.toFact()) >= memoryFactRank(existing.toFact()) {
				normalizedInferences[inference.ID] = inference
			}
			continue
		}
		normalizedInferences[inference.ID] = inference
	}
	m.InferenceItems = mapInferences(normalizedInferences)

	derivedFacts := make([]MemoryFact, 0, len(m.ObservationItems)+len(m.InferenceItems))
	for _, observation := range m.ObservationItems {
		fact := memoryFactFromObservation(observation)
		if fact.Key == "" || fact.Value == "" {
			continue
		}
		derivedFacts = append(derivedFacts, fact)
	}
	for _, inference := range m.InferenceItems {
		fact := inference.toFact()
		if fact.Key == "" && fact.Value == "" {
			continue
		}
		derivedFacts = append(derivedFacts, fact)
	}

	normalizedFacts := make(map[string]MemoryFact, len(m.FactItems)+len(derivedFacts))
	for _, fact := range m.FactItems {
		fact = m.normalizeFact(fact)
		if fact.Key == "" && fact.Value == "" {
			continue
		}
		if mergeIdentity, ok := memoryMergeableFactIdentity(normalizedFacts, fact); ok {
			normalizedFacts[mergeIdentity] = m.normalizeFact(memoryMergeFact(normalizedFacts[mergeIdentity], fact))
			continue
		}
		identity := memoryFactIdentity(fact)
		if existing, ok := normalizedFacts[identity]; ok {
			normalizedFacts[identity] = m.normalizeFact(memoryMergeFact(existing, fact))
			if memoryFactRank(fact) > memoryFactRank(existing) {
				normalizedFacts[identity] = m.normalizeFact(memoryMergeFact(fact, existing))
			}
			continue
		}
		normalizedFacts[identity] = fact
	}
	for _, fact := range derivedFacts {
		fact = m.normalizeFact(fact)
		if fact.Key == "" && fact.Value == "" {
			continue
		}
		if mergeIdentity, ok := memoryMergeableFactIdentity(normalizedFacts, fact); ok {
			normalizedFacts[mergeIdentity] = m.normalizeFact(memoryMergeFact(normalizedFacts[mergeIdentity], fact))
			continue
		}
		identity := memoryFactIdentity(fact)
		if existing, ok := normalizedFacts[identity]; ok {
			normalizedFacts[identity] = m.normalizeFact(memoryMergeFact(existing, fact))
			if memoryFactRank(fact) > memoryFactRank(existing) {
				normalizedFacts[identity] = m.normalizeFact(memoryMergeFact(fact, existing))
			}
			continue
		}
		normalizedFacts[identity] = fact
	}
	m.FactItems = mapFacts(normalizedFacts)
	m.UpdatedAt = time.Now().UTC()
}

func (m *AssistantMemory) normalizeObservation(observation MemoryObservation) MemoryObservation {
	observation.Scope = strings.ToLower(strings.TrimSpace(observation.Scope))
	observation.Subject = strings.TrimSpace(observation.Subject)
	observation.ContactID = strings.TrimSpace(observation.ContactID)
	observation.ContactAlias = strings.TrimSpace(observation.ContactAlias)
	observation.Key = strings.TrimSpace(observation.Key)
	observation.Summary = strings.TrimSpace(observation.Summary)
	observation.Value = strings.TrimSpace(observation.Value)
	observation.Evidence = strings.TrimSpace(observation.Evidence)
	observation.SourceType = strings.ToLower(strings.TrimSpace(observation.SourceType))
	observation.SourceID = strings.TrimSpace(observation.SourceID)
	observation.EvidenceRefs = assistantNormalizeStringList(observation.EvidenceRefs)
	observation.SourceObservationIDs = assistantNormalizeStringList(observation.SourceObservationIDs)
	observation.Confidence = normalizeConfidence(observation.Confidence)
	observation.Verification = normalizeVerification(observation.Verification)
	observation.ObservedAt = normalizeMemoryTime(observation.ObservedAt)
	if observation.ObservedAt.IsZero() {
		observation.ObservedAt = time.Now().UTC()
	}
	observation.EffectiveAt = normalizeMemoryTime(observation.EffectiveAt)
	observation.EffectiveStart = normalizeMemoryTime(observation.EffectiveStart)
	observation.EffectiveEnd = normalizeMemoryTime(observation.EffectiveEnd)
	if observation.EffectiveAt.IsZero() && !observation.EffectiveStart.IsZero() {
		observation.EffectiveAt = observation.EffectiveStart
	}
	if observation.EffectiveStart.IsZero() && !observation.EffectiveAt.IsZero() {
		observation.EffectiveStart = observation.EffectiveAt
	}

	if observation.ContactID == "" && observation.ContactAlias != "" {
		if resolved := m.resolveContactIDUnlocked(observation.ContactAlias); resolved != "" {
			observation.ContactID = resolved
		} else {
			observation.ContactID = memoryContactIDFromAlias(observation.ContactAlias)
			m.upsertContactAlias(observation.ContactID, observation.ContactAlias)
		}
	} else if observation.ContactID != "" && observation.ContactAlias != "" {
		m.upsertContactAlias(observation.ContactID, observation.ContactAlias)
	}

	observation.Kind = memoryNormalizeKind(observation)
	observation.Bucket = memoryNormalizeBucket(
		observation.Bucket,
		observation.Kind,
		observation,
		time.Now().UTC(),
	)
	observation.Importance = memoryNormalizeImportance(observation.Importance, observation)
	observation.Bucket = memoryRolloverBucket(observation.Bucket, observation.Kind, observation.EffectiveStart, observation.EffectiveEnd, time.Now().UTC())
	observation.Confidence = memoryDecayConfidence(observation.Confidence, observation.Verification, observation.Bucket, observation.ObservedAt, time.Time{}, time.Now().UTC())
	observation.RetrievalText = memoryNormalizeRetrievalText(
		observation.RetrievalText,
		memorySearchTextFromObservation(observation),
	)
	observation.InferenceReason = strings.TrimSpace(observation.InferenceReason)
	observation.ID = strings.TrimSpace(observation.ID)
	if observation.ID == "" {
		observation.ID = memoryObservationID(observation)
	}
	return observation
}

func (m *AssistantMemory) normalizeFact(fact MemoryFact) MemoryFact {
	fact.Scope = strings.ToLower(strings.TrimSpace(fact.Scope))
	fact.Subject = strings.TrimSpace(fact.Subject)
	fact.ContactID = strings.TrimSpace(fact.ContactID)
	fact.ContactAlias = strings.TrimSpace(fact.ContactAlias)
	fact.Key = strings.TrimSpace(fact.Key)
	fact.Summary = strings.TrimSpace(fact.Summary)
	fact.Value = strings.TrimSpace(fact.Value)
	fact.Evidence = strings.TrimSpace(fact.Evidence)
	fact.SourceType = strings.ToLower(strings.TrimSpace(fact.SourceType))
	fact.SourceID = strings.TrimSpace(fact.SourceID)
	fact.SourceObservationIDs = assistantNormalizeStringList(fact.SourceObservationIDs)
	fact.EvidenceRefs = assistantNormalizeStringList(fact.EvidenceRefs)
	fact.Confidence = normalizeConfidence(fact.Confidence)
	fact.Verification = normalizeVerification(fact.Verification)
	fact.ObservedAt = normalizeMemoryTime(fact.ObservedAt)
	fact.LastUsedAt = normalizeMemoryTime(fact.LastUsedAt)
	fact.EffectiveAt = normalizeMemoryTime(fact.EffectiveAt)
	fact.EffectiveStart = normalizeMemoryTime(fact.EffectiveStart)
	fact.EffectiveEnd = normalizeMemoryTime(fact.EffectiveEnd)
	if fact.EffectiveAt.IsZero() && !fact.EffectiveStart.IsZero() {
		fact.EffectiveAt = fact.EffectiveStart
	}
	if fact.EffectiveStart.IsZero() && !fact.EffectiveAt.IsZero() {
		fact.EffectiveStart = fact.EffectiveAt
	}
	if fact.ContactID == "" && fact.ContactAlias != "" {
		if resolved := m.resolveContactIDUnlocked(fact.ContactAlias); resolved != "" {
			fact.ContactID = resolved
		} else {
			fact.ContactID = memoryContactIDFromAlias(fact.ContactAlias)
			m.upsertContactAlias(fact.ContactID, fact.ContactAlias)
		}
	}
	fact.Kind = memoryNormalizeKindFromFact(fact)
	fact.Bucket = memoryNormalizeBucket(
		fact.Bucket,
		fact.Kind,
		MemoryObservation{
			ContactID:      fact.ContactID,
			ContactAlias:   fact.ContactAlias,
			Key:            fact.Key,
			Value:          fact.Value,
			Evidence:       fact.Evidence,
			SourceType:     fact.SourceType,
			ObservedAt:     fact.ObservedAt,
			EffectiveAt:    fact.EffectiveAt,
			EffectiveStart: fact.EffectiveStart,
			EffectiveEnd:   fact.EffectiveEnd,
			Confidence:     fact.Confidence,
			Verification:   fact.Verification,
			Kind:           fact.Kind,
			Bucket:         fact.Bucket,
			Importance:     fact.Importance,
		},
		time.Now().UTC(),
	)
	fact.Importance = memoryNormalizeImportance(
		fact.Importance,
		MemoryObservation{
			ContactID:      fact.ContactID,
			ContactAlias:   fact.ContactAlias,
			Key:            fact.Key,
			Value:          fact.Value,
			Evidence:       fact.Evidence,
			SourceType:     fact.SourceType,
			ObservedAt:     fact.ObservedAt,
			EffectiveAt:    fact.EffectiveAt,
			EffectiveStart: fact.EffectiveStart,
			EffectiveEnd:   fact.EffectiveEnd,
			Confidence:     fact.Confidence,
			Verification:   fact.Verification,
			Kind:           fact.Kind,
			Bucket:         fact.Bucket,
			Importance:     fact.Importance,
		},
	)
	fact.Bucket = memoryRolloverBucket(fact.Bucket, fact.Kind, fact.EffectiveStart, fact.EffectiveEnd, time.Now().UTC())
	fact.Confidence = memoryDecayConfidence(fact.Confidence, fact.Verification, fact.Bucket, fact.ObservedAt, fact.LastUsedAt, time.Now().UTC())
	fact.RetrievalText = memoryNormalizeRetrievalText(
		fact.RetrievalText,
		memorySearchTextFromFact(fact),
	)
	fact.InferenceReason = strings.TrimSpace(fact.InferenceReason)
	fact.ID = strings.TrimSpace(fact.ID)
	if fact.ID == "" {
		fact.ID = memoryFactIdentity(fact)
	}
	return fact
}

func (m *AssistantMemory) normalizeInference(inference MemoryInference) MemoryInference {
	inference.ID = strings.TrimSpace(inference.ID)
	inference.Kind = normalizeKind(inference.Kind)
	inference.Bucket = normalizeBucket(inference.Bucket)
	inference.Scope = strings.ToLower(strings.TrimSpace(inference.Scope))
	inference.Subject = strings.TrimSpace(inference.Subject)
	inference.ContactID = strings.TrimSpace(inference.ContactID)
	inference.ContactAlias = strings.TrimSpace(inference.ContactAlias)
	inference.Key = strings.TrimSpace(inference.Key)
	inference.Summary = strings.TrimSpace(inference.Summary)
	inference.Value = strings.TrimSpace(inference.Value)
	inference.Evidence = strings.TrimSpace(inference.Evidence)
	inference.EvidenceRefs = assistantNormalizeStringList(inference.EvidenceRefs)
	inference.SourceType = strings.ToLower(strings.TrimSpace(inference.SourceType))
	inference.SourceID = strings.TrimSpace(inference.SourceID)
	inference.ObservedAt = normalizeMemoryTime(inference.ObservedAt)
	if inference.ObservedAt.IsZero() {
		inference.ObservedAt = time.Now().UTC()
	}
	inference.EffectiveStart = normalizeMemoryTime(inference.EffectiveStart)
	inference.EffectiveEnd = normalizeMemoryTime(inference.EffectiveEnd)
	inference.Confidence = normalizeConfidence(inference.Confidence)
	inference.Verification = normalizeVerification(inference.Verification)
	inference.Importance = memoryNormalizeImportance(inference.Importance, MemoryObservation{
		ContactID:      inference.ContactID,
		ContactAlias:   inference.ContactAlias,
		Key:            inference.Key,
		Summary:        inference.Summary,
		Value:          inference.Value,
		Evidence:       inference.Evidence,
		SourceType:     inference.SourceType,
		ObservedAt:     inference.ObservedAt,
		EffectiveStart: inference.EffectiveStart,
		EffectiveEnd:   inference.EffectiveEnd,
		Confidence:     inference.Confidence,
		Verification:   inference.Verification,
		Kind:           inference.Kind,
		Bucket:         inference.Bucket,
		Importance:     inference.Importance,
	})
	inference.Bucket = memoryRolloverBucket(inference.Bucket, inference.Kind, inference.EffectiveStart, inference.EffectiveEnd, time.Now().UTC())
	inference.Confidence = memoryDecayConfidence(inference.Confidence, inference.Verification, inference.Bucket, inference.ObservedAt, time.Time{}, time.Now().UTC())
	if inference.ContactID == "" && inference.ContactAlias != "" {
		if resolved := m.resolveContactIDUnlocked(inference.ContactAlias); resolved != "" {
			inference.ContactID = resolved
		} else {
			inference.ContactID = memoryContactIDFromAlias(inference.ContactAlias)
			m.upsertContactAlias(inference.ContactID, inference.ContactAlias)
		}
	}
	if inference.ID == "" {
		inference.ID = memoryHash(
			inference.Subject,
			inference.ContactID,
			inference.ContactAlias,
			inference.Key,
			inference.Summary,
			inference.Value,
			inference.SourceType,
			inference.SourceID,
			inference.ObservedAt.UTC().Format(time.RFC3339Nano),
		)
	}
	return inference
}

func memoryFactFromObservation(observation MemoryObservation) MemoryFact {
	return MemoryFact{
		ID: memoryFactIdentity(MemoryFact{
			Scope:                observation.Scope,
			Subject:              observation.Subject,
			ContactID:            observation.ContactID,
			ContactAlias:         observation.ContactAlias,
			Key:                  observation.Key,
			Summary:              observation.Summary,
			Value:                observation.Value,
			SourceType:           observation.SourceType,
			SourceID:             observation.SourceID,
			ObservedAt:           observation.ObservedAt,
			EffectiveAt:          observation.EffectiveAt,
			EffectiveStart:       observation.EffectiveStart,
			EffectiveEnd:         observation.EffectiveEnd,
			Confidence:           observation.Confidence,
			Verification:         observation.Verification,
			Kind:                 observation.Kind,
			Bucket:               observation.Bucket,
			Importance:           observation.Importance,
			SourceObservationIDs: []string{observation.ID},
		}),
		Scope:                observation.Scope,
		Subject:              observation.Subject,
		ContactID:            observation.ContactID,
		ContactAlias:         observation.ContactAlias,
		Key:                  observation.Key,
		Summary:              observation.Summary,
		Value:                observation.Value,
		Evidence:             observation.Evidence,
		EvidenceRefs:         append([]string(nil), observation.EvidenceRefs...),
		SourceType:           observation.SourceType,
		SourceID:             observation.SourceID,
		SourceObservationIDs: []string{observation.ID},
		ObservedAt:           observation.ObservedAt,
		EffectiveAt:          observation.EffectiveAt,
		EffectiveStart:       observation.EffectiveStart,
		EffectiveEnd:         observation.EffectiveEnd,
		Confidence:           observation.Confidence,
		Verification:         observation.Verification,
		Kind:                 observation.Kind,
		Bucket:               observation.Bucket,
		Importance:           observation.Importance,
		InferenceReason:      observation.InferenceReason,
	}
}

func (observation MemoryObservation) toFact() MemoryFact {
	return memoryFactFromObservation(observation)
}

func (fact MemoryFact) toFact() MemoryFact {
	return fact
}

func memoryFactFromInference(inference MemoryInference) MemoryFact {
	return MemoryFact{
		ID:              memoryFactIdentity(inference.toFact()),
		Scope:           strings.TrimSpace(inference.Scope),
		Subject:         strings.TrimSpace(inference.Subject),
		ContactID:       strings.TrimSpace(inference.ContactID),
		ContactAlias:    strings.TrimSpace(inference.ContactAlias),
		Key:             strings.TrimSpace(inference.Key),
		Summary:         strings.TrimSpace(inference.Summary),
		Value:           strings.TrimSpace(inference.Value),
		Evidence:        strings.TrimSpace(inference.Evidence),
		EvidenceRefs:    assistantNormalizeStringList(inference.EvidenceRefs),
		SourceType:      strings.ToLower(strings.TrimSpace(inference.SourceType)),
		SourceID:        strings.TrimSpace(inference.SourceID),
		ObservedAt:      normalizeMemoryTime(inference.ObservedAt),
		EffectiveStart:  normalizeMemoryTime(inference.EffectiveStart),
		EffectiveEnd:    normalizeMemoryTime(inference.EffectiveEnd),
		Confidence:      normalizeConfidence(inference.Confidence),
		Verification:    normalizeVerification(inference.Verification),
		Kind:            normalizeKind(inference.Kind),
		Bucket:          normalizeBucket(inference.Bucket),
		Importance:      clampInt(inference.Importance, 1, 100),
		InferenceReason: strings.TrimSpace(inference.InferenceReason),
	}
}

func (i MemoryInference) toFact() MemoryFact {
	value := strings.TrimSpace(i.Value)
	summary := strings.TrimSpace(i.Summary)
	if value == "" {
		value = summary
	}
	if summary == "" {
		summary = value
	}
	return MemoryFact{
		ID:              strings.TrimSpace(i.ID),
		Scope:           strings.TrimSpace(i.Scope),
		Subject:         strings.TrimSpace(i.Subject),
		ContactID:       strings.TrimSpace(i.ContactID),
		ContactAlias:    strings.TrimSpace(i.ContactAlias),
		Key:             strings.TrimSpace(i.Key),
		Summary:         summary,
		Value:           value,
		Evidence:        strings.TrimSpace(i.Evidence),
		EvidenceRefs:    assistantNormalizeStringList(i.EvidenceRefs),
		SourceType:      strings.ToLower(strings.TrimSpace(i.SourceType)),
		SourceID:        strings.TrimSpace(i.SourceID),
		ObservedAt:      normalizeMemoryTime(i.ObservedAt),
		EffectiveStart:  normalizeMemoryTime(i.EffectiveStart),
		EffectiveEnd:    normalizeMemoryTime(i.EffectiveEnd),
		Confidence:      normalizeConfidence(i.Confidence),
		Verification:    normalizeVerification(i.Verification),
		Kind:            normalizeKind(i.Kind),
		Bucket:          normalizeBucket(i.Bucket),
		Importance:      clampInt(i.Importance, 1, 100),
		InferenceReason: strings.TrimSpace(i.InferenceReason),
		RetrievalText:   strings.TrimSpace(i.RetrievalText),
	}
}

func memoryObservationID(observation MemoryObservation) string {
	parts := []string{
		observation.Scope,
		observation.ContactID,
		observation.ContactAlias,
		observation.Key,
		observation.Value,
		observation.SourceType,
		observation.SourceID,
		observation.ObservedAt.UTC().Format(time.RFC3339Nano),
		observation.EffectiveAt.UTC().Format(time.RFC3339Nano),
	}
	return "obs-" + memoryHash(parts...)
}

func memoryFactIdentity(fact MemoryFact) string {
	parts := []string{
		fact.Scope,
		fact.ContactID,
		fact.ContactAlias,
		fact.Key,
		fact.Value,
		fact.SourceType,
		fact.SourceID,
		fact.ObservedAt.UTC().Format(time.RFC3339Nano),
		fact.EffectiveAt.UTC().Format(time.RFC3339Nano),
		fact.EffectiveStart.UTC().Format(time.RFC3339Nano),
		fact.EffectiveEnd.UTC().Format(time.RFC3339Nano),
	}
	if len(fact.SourceObservationIDs) > 0 {
		parts = append(parts, strings.Join(fact.SourceObservationIDs, ","))
	}
	return "fact-" + memoryHash(parts...)
}

func memoryHash(parts ...string) string {
	h := sha1.New()
	_, _ = h.Write([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(h.Sum(nil)[:8])
}

func memoryObservationRank(item MemoryObservation) int {
	return memoryFactRank(memoryFactFromObservation(item))
}

func memoryFactRank(item MemoryFact) int {
	score := 0
	switch normalizeBucket(item.Bucket) {
	case MemoryBucketDurable:
		score += 500
	case MemoryBucketActive:
		score += 400
	case MemoryBucketScheduled:
		score += 250
	case MemoryBucketTentative:
		score += 120
	case MemoryBucketExpired:
		score += 20
	}
	switch normalizeKind(item.Kind) {
	case MemoryKindProfile:
		score += 90
	case MemoryKindSituation:
		score += 80
	case MemoryKindProject:
		score += 75
	case MemoryKindPreference:
		score += 70
	case MemoryKindRelationship:
		score += 65
	case MemoryKindEpisode:
		score += 50
	case MemoryKindNote:
		score += 25
	default:
		score += 30
	}
	score += normalizeImportance(item.Importance) * 2
	switch normalizeVerification(item.Verification) {
	case MemoryVerificationUserConfirmed:
		score += 500
	case MemoryVerificationVerified:
		score += 420
	case MemoryVerificationToolVerified:
		score += 330
	case MemoryVerificationInferred:
		score += 180
	}
	switch normalizeConfidence(item.Confidence) {
	case MemoryConfidenceHigh:
		score += 80
	case MemoryConfidenceMedium:
		score += 45
	case MemoryConfidenceLow:
		score += 15
	}
	switch strings.ToLower(strings.TrimSpace(item.SourceType)) {
	case "user":
		score += 70
	case "gmail", "calendar", "browser", "form", "note", "journal":
		score += 35
	}
	score += memoryRecencyBoost(item.ObservedAt)
	score += memoryTimingBoost(item.EffectiveStart, item.EffectiveEnd)
	return score
}

func memoryNormalizeBucket(bucket MemoryBucket, kind MemoryKind, observation MemoryObservation, now time.Time) MemoryBucket {
	bucket = normalizeBucket(bucket)
	if bucket != "" {
		return bucket
	}
	if !observation.EffectiveEnd.IsZero() && now.After(observation.EffectiveEnd.UTC()) {
		return MemoryBucketExpired
	}
	if !observation.EffectiveStart.IsZero() && now.Before(observation.EffectiveStart.UTC()) {
		return MemoryBucketScheduled
	}
	if containsAny(normalizeMemoryText(observation.Key), "upcoming", "next", "scheduled", "deadline", "due", "meeting", "appointment", "trip", "flight") {
		return MemoryBucketScheduled
	}
	if containsAny(normalizeMemoryText(observation.Key), "current", "now", "today", "active", "ongoing") {
		return MemoryBucketActive
	}
	if memoryVerificationIsStrong(observation.Verification) {
		return MemoryBucketDurable
	}
	if memoryConfidenceIsStrong(observation.Confidence) && normalizeKind(kind) != MemoryKindNote {
		return MemoryBucketDurable
	}
	if normalizeKind(kind) == MemoryKindSituation || normalizeKind(kind) == MemoryKindEpisode {
		return MemoryBucketActive
	}
	return MemoryBucketTentative
}

func memoryNormalizeKind(observation MemoryObservation) MemoryKind {
	return memoryNormalizeKindCore(observation.Scope, observation.Key, observation.Value, observation.SourceType)
}

func memoryNormalizeKindFromFact(fact MemoryFact) MemoryKind {
	return memoryNormalizeKindCore(fact.Scope, fact.Key, fact.Value, fact.SourceType)
}

func memoryNormalizeKindCore(scope, key, value, sourceType string) MemoryKind {
	scope = strings.ToLower(strings.TrimSpace(scope))
	keyText := normalizeMemoryText(key + " " + value)
	switch scope {
	case "contact", "relationship":
		return MemoryKindRelationship
	case "profile":
		return MemoryKindProfile
	case "project":
		return MemoryKindProject
	case "preference":
		return MemoryKindPreference
	case "episode":
		return MemoryKindEpisode
	case "note":
		return MemoryKindNote
	}
	switch {
	case containsAny(keyText, "project", "build", "repo", "tooling", "feature", "implementation", "scaffold"):
		return MemoryKindProject
	case containsAny(keyText, "preference", "likes", "favorite", "favourite", "prefers"):
		return MemoryKindPreference
	case containsAny(keyText, "meeting", "lawyer", "appointment", "training", "army", "schedule", "deadline", "due", "start", "end", "graduat"):
		return MemoryKindSituation
	case containsAny(keyText, "relationship", "partner", "child", "divorce", "family"):
		return MemoryKindRelationship
	case containsAny(keyText, "note", "journal", "remember", "memo"):
		return MemoryKindNote
	case strings.TrimSpace(sourceType) == "calendar":
		return MemoryKindSituation
	case strings.TrimSpace(sourceType) == "gmail":
		return MemoryKindFact
	}
	return MemoryKindFact
}

func memoryNormalizeImportance(importance int, observation MemoryObservation) int {
	if importance > 0 {
		return clampInt(importance, 1, 100)
	}
	switch normalizeVerification(observation.Verification) {
	case MemoryVerificationUserConfirmed:
		importance = 85
	case MemoryVerificationVerified:
		importance = 75
	case MemoryVerificationToolVerified:
		importance = 65
	case MemoryVerificationInferred:
		importance = 45
	default:
		switch normalizeConfidence(observation.Confidence) {
		case MemoryConfidenceHigh:
			importance = 60
		case MemoryConfidenceMedium:
			importance = 40
		case MemoryConfidenceLow:
			importance = 20
		default:
			importance = 30
		}
	}
	switch memoryNormalizeKind(observation) {
	case MemoryKindProfile, MemoryKindRelationship:
		importance += 10
	case MemoryKindSituation, MemoryKindProject:
		importance += 5
	}
	return clampInt(importance, 1, 100)
}

func memoryNormalizeRetrievalText(current, fallback string) string {
	current = strings.TrimSpace(current)
	if current != "" {
		return current
	}
	return strings.TrimSpace(fallback)
}

func memorySearchTextFromObservation(observation MemoryObservation) string {
	parts := []string{
		strings.TrimSpace(observation.Subject),
		strings.TrimSpace(observation.ContactAlias),
		strings.TrimSpace(observation.ContactID),
		string(observation.Kind),
		strings.TrimSpace(observation.Scope),
		strings.TrimSpace(observation.Key),
		strings.TrimSpace(observation.Summary),
		strings.TrimSpace(observation.Value),
		strings.TrimSpace(observation.Evidence),
		strings.TrimSpace(observation.SourceType),
		strings.TrimSpace(observation.SourceID),
		memoryTimeSummary(observation.EffectiveStart, observation.EffectiveEnd, observation.EffectiveAt),
		strings.TrimSpace(observation.InferenceReason),
	}
	if len(observation.EvidenceRefs) > 0 {
		parts = append(parts, strings.Join(observation.EvidenceRefs, " "))
	}
	return strings.Join(filterNonEmpty(parts), " ")
}

func memorySearchTextFromFact(fact MemoryFact) string {
	parts := []string{
		strings.TrimSpace(fact.Subject),
		strings.TrimSpace(fact.ContactAlias),
		strings.TrimSpace(fact.ContactID),
		string(fact.Kind),
		strings.TrimSpace(fact.Scope),
		strings.TrimSpace(fact.Key),
		strings.TrimSpace(fact.Summary),
		strings.TrimSpace(fact.Value),
		strings.TrimSpace(fact.Evidence),
		strings.TrimSpace(fact.SourceType),
		strings.TrimSpace(fact.SourceID),
		memoryTimeSummary(fact.EffectiveStart, fact.EffectiveEnd, fact.EffectiveAt),
		strings.TrimSpace(fact.InferenceReason),
	}
	if len(fact.EvidenceRefs) > 0 {
		parts = append(parts, strings.Join(fact.EvidenceRefs, " "))
	}
	if len(fact.SourceObservationIDs) > 0 {
		parts = append(parts, strings.Join(fact.SourceObservationIDs, " "))
	}
	return strings.Join(filterNonEmpty(parts), " ")
}

func memorySemanticScore(query, text string) float64 {
	query = normalizeMemoryText(query)
	text = normalizeMemoryText(text)
	if query == "" || text == "" {
		return 0
	}
	queryTokens := memoryTokenize(query)
	if len(queryTokens) == 0 {
		return 0
	}
	textTokens := memoryTokenize(text)
	if len(textTokens) == 0 {
		return 0
	}
	textSet := make(map[string]struct{}, len(textTokens))
	for _, token := range textTokens {
		textSet[token] = struct{}{}
	}
	overlap := 0
	for _, token := range queryTokens {
		if !memorySemanticTokenAllowed(token) {
			continue
		}
		if _, ok := textSet[token]; ok {
			overlap++
			continue
		}
		for candidate := range textSet {
			if strings.HasPrefix(candidate, token) || strings.HasPrefix(token, candidate) {
				overlap++
				break
			}
		}
	}
	score := float64(overlap) / float64(len(queryTokens))
	if strings.Contains(text, query) {
		score += 1.5
	}
	for _, token := range queryTokens {
		if !memorySemanticTokenAllowed(token) {
			continue
		}
		if len(token) >= 4 && strings.Contains(text, token) {
			score += 0.2
		}
	}
	return score
}

func memoryTokens(text string) []string {
	return memoryTokenize(text)
}

func memorySemanticTokenAllowed(token string) bool {
	token = strings.ToLower(strings.TrimSpace(token))
	if token == "" {
		return false
	}
	switch token {
	case "a", "an", "and", "are", "as", "at", "be", "been", "being", "but", "by", "do", "does", "done", "for", "from", "had", "has", "have", "how", "i", "in", "is", "it", "me", "my", "of", "on", "or", "our", "she", "the", "their", "them", "there", "this", "that", "to", "was", "we", "were", "what", "when", "where", "which", "who", "why", "with", "you", "your":
		return false
	}
	if len(token) < 3 && token != "go" && token != "ai" && token != "js" && token != "ts" && token != "db" {
		return false
	}
	return true
}

func memoryFactSearchScore(item MemoryFact, query string, tokens []string) int {
	if len(tokens) == 0 {
		tokens = memoryTokenize(query)
	}
	text := memorySearchTextFromFact(item)
	semantic := memorySemanticScore(query, text)
	if semantic <= 0 {
		return 0
	}
	score := int(semantic * 100)
	score += memoryFactRank(item)
	if item.LastUsedAt.IsZero() == false {
		score += 10
	}
	if len(tokens) > 0 {
		score += len(tokens) * 2
	}
	return score
}

func memoryTokenize(text string) []string {
	text = normalizeMemoryText(text)
	if text == "" {
		return nil
	}
	fields := strings.FieldsFunc(text, func(r rune) bool {
		switch {
		case r >= 'a' && r <= 'z':
			return false
		case r >= '0' && r <= '9':
			return false
		default:
			return true
		}
	})
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		out = append(out, field)
	}
	return out
}

func memoryTimeSummary(start, end, at time.Time) string {
	var parts []string
	if !start.IsZero() {
		parts = append(parts, "from "+start.UTC().Format(time.RFC3339))
	}
	if !end.IsZero() {
		parts = append(parts, "to "+end.UTC().Format(time.RFC3339))
	}
	if !at.IsZero() {
		parts = append(parts, "at "+at.UTC().Format(time.RFC3339))
	}
	return strings.Join(parts, " ")
}

func memoryVerificationIsStrong(value MemoryVerification) bool {
	switch normalizeVerification(value) {
	case MemoryVerificationUserConfirmed, MemoryVerificationVerified, MemoryVerificationToolVerified:
		return true
	default:
		return false
	}
}

func memoryConfidenceIsStrong(value MemoryConfidence) bool {
	return normalizeConfidence(value) == MemoryConfidenceHigh
}

func memoryRecencyBoost(at time.Time) int {
	if at.IsZero() {
		return 0
	}
	hours := int(time.Since(at.UTC()).Hours())
	switch {
	case hours <= 1:
		return 40
	case hours <= 24:
		return 30
	case hours <= 72:
		return 18
	case hours <= 24*14:
		return 8
	case hours <= 24*90:
		return 2
	default:
		return 0
	}
}

func memoryTimingBoost(start, end time.Time) int {
	now := time.Now().UTC()
	if !start.IsZero() && now.Before(start.UTC()) {
		return 15
	}
	if !end.IsZero() && now.After(end.UTC()) {
		return -15
	}
	if !start.IsZero() && !end.IsZero() && !now.Before(start.UTC()) && !now.After(end.UTC()) {
		return 20
	}
	return 0
}

func memoryRolloverBucket(bucket MemoryBucket, kind MemoryKind, start, end, now time.Time) MemoryBucket {
	bucket = normalizeBucket(bucket)
	kind = normalizeKind(kind)
	now = now.UTC()
	if !end.IsZero() && now.After(end.UTC()) {
		return MemoryBucketExpired
	}
	if !start.IsZero() && now.Before(start.UTC()) {
		return MemoryBucketScheduled
	}
	if !start.IsZero() && !now.Before(start.UTC()) {
		switch bucket {
		case MemoryBucketScheduled, MemoryBucketTentative:
			switch kind {
			case MemoryKindSituation, MemoryKindEpisode, MemoryKindProject, MemoryKindFact, MemoryKindRelationship:
				return MemoryBucketActive
			}
		}
	}
	return bucket
}

func memoryDecayConfidence(confidence MemoryConfidence, verification MemoryVerification, bucket MemoryBucket, observedAt, lastUsedAt, now time.Time) MemoryConfidence {
	confidence = normalizeConfidence(confidence)
	verification = normalizeVerification(verification)
	bucket = normalizeBucket(bucket)
	now = now.UTC()
	if confidence == MemoryConfidenceUnknown {
		return confidence
	}
	if verification == MemoryVerificationUserConfirmed || verification == MemoryVerificationVerified {
		return confidence
	}
	anchor := observedAt.UTC()
	if !lastUsedAt.IsZero() && lastUsedAt.After(anchor) {
		anchor = lastUsedAt.UTC()
	}
	if anchor.IsZero() {
		return confidence
	}
	age := now.Sub(anchor)
	steps := 0
	switch verification {
	case MemoryVerificationInferred:
		switch {
		case age >= 120*24*time.Hour:
			steps = 3
		case age >= 45*24*time.Hour:
			steps = 2
		case age >= 14*24*time.Hour:
			steps = 1
		}
	case MemoryVerificationToolVerified:
		switch {
		case age >= 365*24*time.Hour:
			steps = 2
		case age >= 120*24*time.Hour:
			steps = 1
		}
	}
	if bucket == MemoryBucketExpired && steps < 1 {
		steps = 1
	}
	return memoryReduceConfidence(confidence, steps)
}

func memoryReduceConfidence(confidence MemoryConfidence, steps int) MemoryConfidence {
	for i := 0; i < steps; i++ {
		switch normalizeConfidence(confidence) {
		case MemoryConfidenceHigh:
			confidence = MemoryConfidenceMedium
		case MemoryConfidenceMedium:
			confidence = MemoryConfidenceLow
		case MemoryConfidenceLow:
			confidence = MemoryConfidenceUnknown
		default:
			return MemoryConfidenceUnknown
		}
	}
	return normalizeConfidence(confidence)
}

func normalizeMemoryTime(value time.Time) time.Time {
	if value.IsZero() {
		return time.Time{}
	}
	return value.UTC()
}

func normalizeBucket(value MemoryBucket) MemoryBucket {
	switch strings.ToLower(strings.TrimSpace(string(value))) {
	case string(MemoryBucketDurable):
		return MemoryBucketDurable
	case string(MemoryBucketActive):
		return MemoryBucketActive
	case string(MemoryBucketScheduled):
		return MemoryBucketScheduled
	case string(MemoryBucketTentative):
		return MemoryBucketTentative
	case string(MemoryBucketExpired):
		return MemoryBucketExpired
	default:
		return ""
	}
}

func normalizeKind(value MemoryKind) MemoryKind {
	switch strings.ToLower(strings.TrimSpace(string(value))) {
	case string(MemoryKindFact):
		return MemoryKindFact
	case string(MemoryKindProfile):
		return MemoryKindProfile
	case string(MemoryKindSituation):
		return MemoryKindSituation
	case string(MemoryKindProject):
		return MemoryKindProject
	case string(MemoryKindPreference):
		return MemoryKindPreference
	case string(MemoryKindEpisode):
		return MemoryKindEpisode
	case string(MemoryKindRelationship):
		return MemoryKindRelationship
	case string(MemoryKindNote):
		return MemoryKindNote
	default:
		return ""
	}
}

func normalizeConfidence(value MemoryConfidence) MemoryConfidence {
	switch strings.ToLower(strings.TrimSpace(string(value))) {
	case string(MemoryConfidenceHigh):
		return MemoryConfidenceHigh
	case string(MemoryConfidenceMedium):
		return MemoryConfidenceMedium
	case string(MemoryConfidenceLow):
		return MemoryConfidenceLow
	case string(MemoryConfidenceUnknown):
		return MemoryConfidenceUnknown
	default:
		return MemoryConfidenceUnknown
	}
}

func normalizeVerification(value MemoryVerification) MemoryVerification {
	switch strings.ToLower(strings.TrimSpace(string(value))) {
	case string(MemoryVerificationUserConfirmed):
		return MemoryVerificationUserConfirmed
	case string(MemoryVerificationToolVerified):
		return MemoryVerificationToolVerified
	case string(MemoryVerificationVerified):
		return MemoryVerificationVerified
	case string(MemoryVerificationInferred):
		return MemoryVerificationInferred
	default:
		return MemoryVerificationInferred
	}
}

func normalizeImportance(value int) int {
	return clampInt(value, 0, 100)
}

func normalizeMemoryText(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	value = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= '0' && r <= '9':
			return r
		default:
			return ' '
		}
	}, value)
	return strings.Join(strings.Fields(value), " ")
}

func filterNonEmpty(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		out = append(out, value)
	}
	return out
}

func containsAny(text string, terms ...string) bool {
	for _, term := range terms {
		term = strings.ToLower(strings.TrimSpace(term))
		if term == "" {
			continue
		}
		if strings.Contains(text, term) {
			return true
		}
	}
	return false
}

func clampInt(value, minValue, maxValue int) int {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func memoryContactIDFromAlias(alias string) string {
	alias = normalizeMemoryText(alias)
	if alias == "" {
		return "contact-" + memoryHash("unknown")
	}
	return "contact-" + memoryHash(alias)
}

func (m *AssistantMemory) upsertContactAlias(contactID, alias string) {
	contactID = strings.TrimSpace(contactID)
	alias = strings.TrimSpace(alias)
	if m == nil || contactID == "" || alias == "" {
		return
	}
	if m.ContactsByID == nil {
		m.ContactsByID = make(map[string]AssistantContact)
	}
	contact := m.ContactsByID[contactID]
	contact.ID = contactID
	if contact.Label == "" {
		contact.Label = alias
	}
	contact.Aliases = assistantNormalizeStringList(append(contact.Aliases, alias))
	m.ContactsByID[contactID] = contact
}

func (m *AssistantMemory) resolveContactIDUnlocked(alias string) string {
	alias = strings.TrimSpace(alias)
	if alias == "" {
		return ""
	}
	if _, ok := m.ContactsByID[alias]; ok {
		return alias
	}
	normalized := normalizeMemoryText(alias)
	for id, contact := range m.ContactsByID {
		if strings.EqualFold(strings.TrimSpace(contact.Label), alias) {
			return id
		}
		for _, candidate := range contact.Aliases {
			if normalizeMemoryText(candidate) == normalized {
				return id
			}
		}
	}
	return ""
}

func normalizeContact(id string, contact AssistantContact) AssistantContact {
	id = strings.TrimSpace(id)
	contact.ID = strings.TrimSpace(contact.ID)
	if contact.ID == "" {
		contact.ID = id
	}
	contact.Label = strings.TrimSpace(contact.Label)
	contact.Aliases = assistantNormalizeStringList(contact.Aliases)
	if contact.Label == "" && len(contact.Aliases) > 0 {
		contact.Label = contact.Aliases[0]
	}
	return contact
}

func mergeContacts(existing, incoming AssistantContact) AssistantContact {
	if existing.ID == "" {
		return incoming
	}
	if incoming.ID == "" {
		return existing
	}
	if existing.Label == "" {
		existing.Label = incoming.Label
	}
	existing.Aliases = assistantNormalizeStringList(append(existing.Aliases, incoming.Aliases...))
	if existing.Label == "" && len(existing.Aliases) > 0 {
		existing.Label = existing.Aliases[0]
	}
	return existing
}

func memoryObservationLifecycleMatch(existing, next MemoryObservation) bool {
	return memoryLifecycleSignatureFromObservation(existing) != "" &&
		memoryLifecycleSignatureFromObservation(existing) == memoryLifecycleSignatureFromObservation(next)
}

func memoryInferenceLifecycleMatch(existing, next MemoryInference) bool {
	return memoryLifecycleSignatureFromFact(existing.toFact()) != "" &&
		memoryLifecycleSignatureFromFact(existing.toFact()) == memoryLifecycleSignatureFromFact(next.toFact())
}

func memoryObservationComparable(existing, next MemoryObservation) bool {
	return memoryFactComparable(memoryFactFromObservation(existing), memoryFactFromObservation(next)) &&
		memoryFactNameSimilar(memoryFactComparableName(memoryFactFromObservation(existing)), memoryFactComparableName(memoryFactFromObservation(next)))
}

func memoryInferenceComparable(existing, next MemoryInference) bool {
	return memoryFactComparable(existing.toFact(), next.toFact()) &&
		memoryFactNameSimilar(memoryFactComparableName(existing.toFact()), memoryFactComparableName(next.toFact()))
}

func memoryObservationEquivalentValue(existing, next MemoryObservation) bool {
	return normalizeMemoryText(existing.Value) == normalizeMemoryText(next.Value) &&
		normalizeMemoryText(existing.Summary) == normalizeMemoryText(next.Summary)
}

func memoryInferenceEquivalentValue(existing, next MemoryInference) bool {
	return normalizeMemoryText(existing.toFact().Value) == normalizeMemoryText(next.toFact().Value) &&
		normalizeMemoryText(existing.toFact().Summary) == normalizeMemoryText(next.toFact().Summary)
}

func memoryObservationSemanticEquivalent(existing, next MemoryObservation) bool {
	return memoryFactSemanticEquivalent(memoryFactFromObservation(existing), memoryFactFromObservation(next))
}

func memoryInferenceSemanticEquivalent(existing, next MemoryInference) bool {
	return memoryFactSemanticEquivalent(existing.toFact(), next.toFact())
}

func memoryObservationShouldExpire(existing, next MemoryObservation) bool {
	if next.ObservedAt.IsZero() || (!existing.ObservedAt.IsZero() && next.ObservedAt.Before(existing.ObservedAt)) {
		return false
	}
	return memoryFactShouldExpire(memoryFactFromObservation(existing), memoryFactFromObservation(next))
}

func memoryInferenceShouldExpire(existing, next MemoryInference) bool {
	if next.ObservedAt.IsZero() || (!existing.ObservedAt.IsZero() && next.ObservedAt.Before(existing.ObservedAt)) {
		return false
	}
	return memoryFactShouldExpire(existing.toFact(), next.toFact())
}

func memoryFactLifecycleMatch(existing, next MemoryFact) bool {
	return memoryLifecycleSignatureFromFact(existing) != "" &&
		memoryLifecycleSignatureFromFact(existing) == memoryLifecycleSignatureFromFact(next)
}

func memoryFactEquivalentValue(existing, next MemoryFact) bool {
	return memoryComparableDimension(existing.Value, next.Value) &&
		memoryComparableDimension(existing.Summary, next.Summary)
}

func memoryFactComparable(existing, next MemoryFact) bool {
	if !memoryComparableOrEmpty(existing.Scope, next.Scope) {
		return false
	}
	if normalizeKind(existing.Kind) != normalizeKind(next.Kind) {
		return false
	}
	if !memoryComparableOrEmpty(memoryFactComparableContact(existing), memoryFactComparableContact(next)) {
		return false
	}
	return true
}

func memoryFactSemanticEquivalent(existing, next MemoryFact) bool {
	if !memoryFactComparable(existing, next) {
		return false
	}
	if !memoryFactNameSimilar(memoryFactComparableName(existing), memoryFactComparableName(next)) && !memoryFactAliasLike(existing, next) {
		return false
	}
	return memoryFactDetailSimilar(existing, next)
}

func memoryFactReplacementConflict(existing, next MemoryFact) bool {
	if !memoryFactComparable(existing, next) {
		return false
	}
	if memoryFactEquivalentValue(existing, next) || memoryFactSameSchedule(existing, next) || memoryNonEmptyComparableDimension(existing.Value, next.Value) {
		return false
	}
	if normalizeKind(existing.Kind) == MemoryKindProject && memoryProjectValueOverlap(existing.Value, next.Value) >= 2 {
		return false
	}
	return memoryFactShouldExpire(existing, next) || memoryFactShouldExpire(next, existing)
}

func memoryFactShouldExpire(existing, next MemoryFact) bool {
	if normalizeMemoryText(existing.Value) == normalizeMemoryText(next.Value) &&
		normalizeMemoryText(existing.Summary) == normalizeMemoryText(next.Summary) {
		return false
	}
	if existing.Bucket == MemoryBucketExpired {
		return false
	}
	kind := normalizeKind(existing.Kind)
	switch kind {
	case MemoryKindSituation, MemoryKindEpisode, MemoryKindProject:
		return true
	}
	switch normalizeBucket(existing.Bucket) {
	case MemoryBucketActive, MemoryBucketScheduled, MemoryBucketTentative:
		return true
	default:
		return false
	}
}

func memoryExpireObservation(existing MemoryObservation, at time.Time) MemoryObservation {
	at = normalizeMemoryTime(at)
	if at.IsZero() {
		at = time.Now().UTC()
	}
	if existing.EffectiveEnd.IsZero() || existing.EffectiveEnd.After(at) {
		existing.EffectiveEnd = at
	}
	existing.Bucket = MemoryBucketExpired
	return existing
}

func memoryExpireInference(existing MemoryInference, at time.Time) MemoryInference {
	at = normalizeMemoryTime(at)
	if at.IsZero() {
		at = time.Now().UTC()
	}
	if existing.EffectiveEnd.IsZero() || existing.EffectiveEnd.After(at) {
		existing.EffectiveEnd = at
	}
	existing.Bucket = MemoryBucketExpired
	return existing
}

func memoryExpireFact(existing MemoryFact, at time.Time) MemoryFact {
	at = normalizeMemoryTime(at)
	if at.IsZero() {
		at = time.Now().UTC()
	}
	if existing.EffectiveEnd.IsZero() || existing.EffectiveEnd.After(at) {
		existing.EffectiveEnd = at
	}
	existing.Bucket = MemoryBucketExpired
	return existing
}

func memoryMergeObservation(existing, next MemoryObservation) MemoryObservation {
	if next.ObservedAt.After(existing.ObservedAt) {
		existing.ObservedAt = next.ObservedAt
	}
	if existing.EffectiveStart.IsZero() || (!next.EffectiveStart.IsZero() && next.EffectiveStart.Before(existing.EffectiveStart)) {
		existing.EffectiveStart = next.EffectiveStart
	}
	if existing.EffectiveAt.IsZero() || (!next.EffectiveAt.IsZero() && next.EffectiveAt.Before(existing.EffectiveAt)) {
		existing.EffectiveAt = next.EffectiveAt
	}
	if existing.EffectiveEnd.IsZero() || (!next.EffectiveEnd.IsZero() && next.EffectiveEnd.After(existing.EffectiveEnd)) {
		existing.EffectiveEnd = next.EffectiveEnd
	}
	existing.Summary = memoryPreferLongerText(existing.Summary, next.Summary)
	existing.Value = memoryPreferLongerText(existing.Value, next.Value)
	existing.Evidence = memoryMergeEvidence(existing.Evidence, next.Evidence)
	existing.EvidenceRefs = assistantNormalizeStringList(append(existing.EvidenceRefs, next.EvidenceRefs...))
	existing.SourceObservationIDs = assistantNormalizeStringList(append(existing.SourceObservationIDs, next.SourceObservationIDs...))
	existing.Importance = memoryMergeImportance(existing.Importance, next.Importance)
	existing.Confidence = memoryStrongerConfidence(existing.Confidence, next.Confidence)
	existing.Verification = memoryStrongerVerification(existing.Verification, next.Verification)
	existing.Bucket = memoryMergeBucket(existing.Bucket, next.Bucket)
	existing.SourceType = memoryPreferLatestSource(existing.SourceType, next.SourceType)
	existing.SourceID = memoryPreferLatestSourceID(existing.SourceID, next.SourceID)
	existing.RetrievalText = ""
	return existing
}

func memoryMergeInference(existing, next MemoryInference) MemoryInference {
	if next.ObservedAt.After(existing.ObservedAt) {
		existing.ObservedAt = next.ObservedAt
	}
	if existing.EffectiveStart.IsZero() || (!next.EffectiveStart.IsZero() && next.EffectiveStart.Before(existing.EffectiveStart)) {
		existing.EffectiveStart = next.EffectiveStart
	}
	if existing.EffectiveEnd.IsZero() || (!next.EffectiveEnd.IsZero() && next.EffectiveEnd.After(existing.EffectiveEnd)) {
		existing.EffectiveEnd = next.EffectiveEnd
	}
	existing.Summary = memoryPreferLongerText(existing.Summary, next.Summary)
	existing.Value = memoryPreferLongerText(existing.Value, next.Value)
	existing.Evidence = memoryMergeEvidence(existing.Evidence, next.Evidence)
	existing.EvidenceRefs = assistantNormalizeStringList(append(existing.EvidenceRefs, next.EvidenceRefs...))
	existing.Importance = memoryMergeImportance(existing.Importance, next.Importance)
	existing.Confidence = memoryStrongerConfidence(existing.Confidence, next.Confidence)
	existing.Verification = memoryStrongerVerification(existing.Verification, next.Verification)
	existing.Bucket = memoryMergeBucket(existing.Bucket, next.Bucket)
	existing.SourceType = memoryPreferLatestSource(existing.SourceType, next.SourceType)
	existing.SourceID = memoryPreferLatestSourceID(existing.SourceID, next.SourceID)
	existing.InferenceReason = memoryPreferLongerText(existing.InferenceReason, next.InferenceReason)
	existing.RetrievalText = ""
	return existing
}

func memoryMergeFact(existing, next MemoryFact) MemoryFact {
	if next.ObservedAt.After(existing.ObservedAt) {
		existing.ObservedAt = next.ObservedAt
	}
	if next.LastUsedAt.After(existing.LastUsedAt) {
		existing.LastUsedAt = next.LastUsedAt
	}
	if existing.EffectiveStart.IsZero() || (!next.EffectiveStart.IsZero() && next.EffectiveStart.Before(existing.EffectiveStart)) {
		existing.EffectiveStart = next.EffectiveStart
	}
	if existing.EffectiveAt.IsZero() || (!next.EffectiveAt.IsZero() && next.EffectiveAt.Before(existing.EffectiveAt)) {
		existing.EffectiveAt = next.EffectiveAt
	}
	if existing.EffectiveEnd.IsZero() || (!next.EffectiveEnd.IsZero() && next.EffectiveEnd.After(existing.EffectiveEnd)) {
		existing.EffectiveEnd = next.EffectiveEnd
	}
	existing.Subject = memoryPreferLongerText(existing.Subject, next.Subject)
	existing.ContactAlias = memoryPreferLongerText(existing.ContactAlias, next.ContactAlias)
	existing.ContactID = memoryPreferLongerText(existing.ContactID, next.ContactID)
	existing.Key = memoryPreferMoreSpecificName(existing.Key, next.Key)
	existing.Summary = memoryPreferLongerText(existing.Summary, next.Summary)
	existing.Value = memoryPreferLongerText(existing.Value, next.Value)
	existing.Evidence = memoryMergeEvidence(existing.Evidence, next.Evidence)
	existing.EvidenceRefs = assistantNormalizeStringList(append(existing.EvidenceRefs, next.EvidenceRefs...))
	existing.SourceObservationIDs = assistantNormalizeStringList(append(existing.SourceObservationIDs, next.SourceObservationIDs...))
	existing.Importance = memoryMergeImportance(existing.Importance, next.Importance)
	existing.Confidence = memoryStrongerConfidence(existing.Confidence, next.Confidence)
	existing.Verification = memoryStrongerVerification(existing.Verification, next.Verification)
	existing.Bucket = memoryMergeBucket(existing.Bucket, next.Bucket)
	existing.SourceType = memoryPreferLatestSource(existing.SourceType, next.SourceType)
	existing.SourceID = memoryPreferLatestSourceID(existing.SourceID, next.SourceID)
	existing.InferenceReason = memoryPreferLongerText(existing.InferenceReason, next.InferenceReason)
	existing.RetrievalText = ""
	return existing
}

func memoryLifecycleSignatureFromObservation(observation MemoryObservation) string {
	return memoryLifecycleSignatureFromFact(memoryFactFromObservation(observation))
}

func memoryLifecycleSignatureFromFact(fact MemoryFact) string {
	name := normalizeMemoryText(fact.Key)
	if name == "" {
		name = normalizeMemoryText(fact.Summary)
	}
	if name == "" {
		return ""
	}
	subject := normalizeMemoryText(fact.Subject)
	contact := normalizeMemoryText(fact.ContactID)
	if contact == "" {
		contact = normalizeMemoryText(fact.ContactAlias)
	}
	scope := normalizeMemoryText(fact.Scope)
	kind := string(normalizeKind(fact.Kind))
	return strings.Join([]string{scope, subject, contact, kind, name}, "|")
}

func memoryMergeableFactIdentity(items map[string]MemoryFact, candidate MemoryFact) (string, bool) {
	for identity, existing := range items {
		if ((memoryFactLifecycleMatch(existing, candidate) && memoryFactEquivalentValue(existing, candidate)) || memoryFactSemanticEquivalent(existing, candidate)) && !memoryFactReplacementConflict(existing, candidate) {
			return identity, true
		}
	}
	return "", false
}

func memoryMergeEvidence(existing, next string) string {
	existing = strings.TrimSpace(existing)
	next = strings.TrimSpace(next)
	switch {
	case existing == "":
		return next
	case next == "":
		return existing
	case strings.EqualFold(existing, next):
		return existing
	case strings.Contains(strings.ToLower(existing), strings.ToLower(next)):
		return existing
	case strings.Contains(strings.ToLower(next), strings.ToLower(existing)):
		return next
	default:
		return existing + " | " + next
	}
}

func memoryPreferLongerText(existing, next string) string {
	existing = strings.TrimSpace(existing)
	next = strings.TrimSpace(next)
	if len(next) > len(existing) {
		return next
	}
	return existing
}

func memoryPreferMoreSpecificName(existing, next string) string {
	existing = strings.TrimSpace(existing)
	next = strings.TrimSpace(next)
	if existing == "" {
		return next
	}
	if next == "" {
		return existing
	}
	if memoryFactNameSpecificity(next) > memoryFactNameSpecificity(existing) {
		return next
	}
	return existing
}

func memoryMergeImportance(existing, next int) int {
	base := existing
	if next > base {
		base = next
	}
	if base == 0 {
		base = next
	}
	return clampInt(base+5, 1, 100)
}

func memoryStrongerConfidence(existing, next MemoryConfidence) MemoryConfidence {
	if memoryConfidenceWeight(next) > memoryConfidenceWeight(existing) {
		return normalizeConfidence(next)
	}
	return normalizeConfidence(existing)
}

func memoryStrongerVerification(existing, next MemoryVerification) MemoryVerification {
	if memoryVerificationWeight(next) > memoryVerificationWeight(existing) {
		return normalizeVerification(next)
	}
	return normalizeVerification(existing)
}

func memoryMergeBucket(existing, next MemoryBucket) MemoryBucket {
	if normalizeBucket(existing) == MemoryBucketExpired || normalizeBucket(next) == MemoryBucketExpired {
		if normalizeBucket(existing) != MemoryBucketExpired {
			return normalizeBucket(existing)
		}
		return normalizeBucket(next)
	}
	if memoryBucketWeight(next) > memoryBucketWeight(existing) {
		return normalizeBucket(next)
	}
	return normalizeBucket(existing)
}

func memoryPreferLatestSource(existing, next string) string {
	next = strings.TrimSpace(next)
	if next != "" {
		return strings.ToLower(next)
	}
	return strings.ToLower(strings.TrimSpace(existing))
}

func memoryComparableDimension(existing, next string) bool {
	existing = normalizeMemoryText(existing)
	next = normalizeMemoryText(next)
	if existing == "" || next == "" {
		return existing == next
	}
	return existing == next
}

func memoryComparableOrEmpty(existing, next string) bool {
	existing = normalizeMemoryText(existing)
	next = normalizeMemoryText(next)
	if existing == "" || next == "" {
		return true
	}
	return existing == next
}

func memoryNonEmptyComparableDimension(existing, next string) bool {
	existing = normalizeMemoryText(existing)
	next = normalizeMemoryText(next)
	if existing == "" || next == "" {
		return false
	}
	return existing == next
}

func memoryFactComparableContact(fact MemoryFact) string {
	if strings.TrimSpace(fact.ContactID) != "" {
		return fact.ContactID
	}
	return fact.ContactAlias
}

func memoryFactComparableName(fact MemoryFact) string {
	if strings.TrimSpace(fact.Key) != "" {
		return fact.Key
	}
	return fact.Summary
}

func memoryFactDetailSimilar(existing, next MemoryFact) bool {
	if memoryNonEmptyComparableDimension(existing.Value, next.Value) {
		return true
	}
	if memoryFactSameSchedule(existing, next) {
		return true
	}
	if normalizeKind(existing.Kind) == MemoryKindProject {
		if memoryProjectValueOverlap(existing.Value, next.Value) >= 2 {
			return true
		}
	}
	if strings.TrimSpace(existing.Value) != "" && strings.TrimSpace(next.Value) != "" {
		return memoryTextSimilar(existing.Value, next.Value)
	}
	if strings.TrimSpace(existing.Summary) != "" && strings.TrimSpace(next.Summary) != "" {
		return memoryTextSimilar(existing.Summary, next.Summary)
	}
	return false
}

func memoryFactSameSchedule(existing, next MemoryFact) bool {
	if !existing.EffectiveStart.IsZero() && !next.EffectiveStart.IsZero() && existing.EffectiveStart.Equal(next.EffectiveStart) {
		return true
	}
	if !existing.EffectiveAt.IsZero() && !next.EffectiveAt.IsZero() && existing.EffectiveAt.Equal(next.EffectiveAt) {
		return true
	}
	if !existing.EffectiveEnd.IsZero() && !next.EffectiveEnd.IsZero() && existing.EffectiveEnd.Equal(next.EffectiveEnd) {
		return true
	}
	return false
}

func memoryFactNameSimilar(existing, next string) bool {
	existing = normalizeMemoryText(existing)
	next = normalizeMemoryText(next)
	if existing == "" || next == "" {
		return false
	}
	if existing == next || strings.Contains(existing, next) || strings.Contains(next, existing) {
		return true
	}
	existingTokens := memorySemanticTokens(existing)
	nextTokens := memorySemanticTokens(next)
	if len(existingTokens) == 0 || len(nextTokens) == 0 {
		return false
	}
	overlap := 0
	for _, left := range existingTokens {
		for _, right := range nextTokens {
			if left == right {
				overlap++
				break
			}
		}
	}
	return overlap >= 2
}

func memoryTextSimilar(existing, next string) bool {
	existing = normalizeMemoryText(existing)
	next = normalizeMemoryText(next)
	if existing == "" || next == "" {
		return false
	}
	if existing != next && memoryTextHasDigits(existing) && memoryTextHasDigits(next) {
		return false
	}
	if existing == next || strings.Contains(existing, next) || strings.Contains(next, existing) {
		return true
	}
	existingTokens := memorySemanticTokens(existing)
	nextTokens := memorySemanticTokens(next)
	if len(existingTokens) == 0 || len(nextTokens) == 0 {
		return false
	}
	overlap := 0
	for _, left := range existingTokens {
		for _, right := range nextTokens {
			if left == right {
				overlap++
				break
			}
		}
	}
	return overlap >= 3
}

func memoryFactAliasLike(existing, next MemoryFact) bool {
	if normalizeKind(existing.Kind) != normalizeKind(next.Kind) {
		return false
	}
	switch normalizeKind(existing.Kind) {
	case MemoryKindProject:
		return memoryProjectLikeName(memoryFactComparableName(existing)) && memoryProjectLikeName(memoryFactComparableName(next))
	default:
		return false
	}
}

func memoryProjectLikeName(value string) bool {
	return strings.Contains(normalizeMemoryText(value), "project")
}

func memoryTextTokenOverlap(existing, next string) int {
	existingTokens := memorySemanticTokens(existing)
	nextTokens := memorySemanticTokens(next)
	if len(existingTokens) == 0 || len(nextTokens) == 0 {
		return 0
	}
	overlap := 0
	for _, left := range existingTokens {
		for _, right := range nextTokens {
			if left == right {
				overlap++
				break
			}
		}
	}
	return overlap
}

func memoryProjectValueOverlap(existing, next string) int {
	existingTokens := memoryProjectTokens(existing)
	nextTokens := memoryProjectTokens(next)
	if len(existingTokens) == 0 || len(nextTokens) == 0 {
		return 0
	}
	overlap := 0
	for _, left := range existingTokens {
		for _, right := range nextTokens {
			if left == right {
				overlap++
				break
			}
		}
	}
	return overlap
}

func memoryProjectTokens(value string) []string {
	stop := map[string]struct{}{
		"current": {}, "currently": {}, "engaged": {}, "focus": {}, "focused": {}, "main": {}, "primary": {}, "project": {}, "working": {}, "user": {},
	}
	all := memorySemanticTokens(value)
	if len(all) == 0 {
		return nil
	}
	out := make([]string, 0, len(all))
	for _, token := range all {
		if _, ok := stop[token]; ok {
			continue
		}
		out = append(out, token)
	}
	return out
}

func memoryTextHasDigits(value string) bool {
	for _, r := range value {
		if r >= '0' && r <= '9' {
			return true
		}
	}
	return false
}

func memoryFactNameSpecificity(value string) int {
	score := 0
	for _, token := range memorySemanticTokens(value) {
		score += len(token)
	}
	return score
}

func memorySemanticTokens(value string) []string {
	fields := strings.Fields(normalizeMemoryText(value))
	if len(fields) == 0 {
		return nil
	}
	stop := map[string]struct{}{
		"a": {}, "an": {}, "and": {}, "for": {}, "in": {}, "my": {}, "of": {}, "on": {}, "the": {}, "to": {}, "with": {}, "me": {}, "our": {}, "your": {},
	}
	out := make([]string, 0, len(fields))
	seen := map[string]struct{}{}
	for _, field := range fields {
		if len(field) < 3 {
			continue
		}
		if _, ok := stop[field]; ok {
			continue
		}
		if _, ok := seen[field]; ok {
			continue
		}
		seen[field] = struct{}{}
		out = append(out, field)
	}
	return out
}

func memoryPreferLatestSourceID(existing, next string) string {
	next = strings.TrimSpace(next)
	if next != "" {
		return next
	}
	return strings.TrimSpace(existing)
}

func memoryConfidenceWeight(value MemoryConfidence) int {
	switch normalizeConfidence(value) {
	case MemoryConfidenceHigh:
		return 4
	case MemoryConfidenceMedium:
		return 3
	case MemoryConfidenceLow:
		return 2
	default:
		return 1
	}
}

func memoryVerificationWeight(value MemoryVerification) int {
	switch normalizeVerification(value) {
	case MemoryVerificationUserConfirmed:
		return 4
	case MemoryVerificationVerified:
		return 3
	case MemoryVerificationToolVerified:
		return 2
	default:
		return 1
	}
}

func memoryBucketWeight(value MemoryBucket) int {
	switch normalizeBucket(value) {
	case MemoryBucketDurable:
		return 5
	case MemoryBucketActive:
		return 4
	case MemoryBucketScheduled:
		return 3
	case MemoryBucketTentative:
		return 2
	case MemoryBucketExpired:
		return 1
	default:
		return 0
	}
}

func mapObservations(items map[string]MemoryObservation) []MemoryObservation {
	out := make([]MemoryObservation, 0, len(items))
	for _, item := range items {
		out = append(out, item)
	}
	sort.SliceStable(out, func(i, j int) bool {
		ri := memoryObservationRank(out[i])
		rj := memoryObservationRank(out[j])
		if ri == rj {
			if out[i].ObservedAt.Equal(out[j].ObservedAt) {
				return out[i].ID < out[j].ID
			}
			return out[i].ObservedAt.Before(out[j].ObservedAt)
		}
		return ri > rj
	})
	return out
}

func mapFacts(items map[string]MemoryFact) []MemoryFact {
	out := make([]MemoryFact, 0, len(items))
	for _, item := range items {
		out = append(out, item)
	}
	sort.SliceStable(out, func(i, j int) bool {
		ri := memoryFactRank(out[i])
		rj := memoryFactRank(out[j])
		if ri == rj {
			if out[i].ObservedAt.Equal(out[j].ObservedAt) {
				if out[i].ContactID == out[j].ContactID {
					if out[i].Key == out[j].Key {
						return out[i].Value < out[j].Value
					}
					return out[i].Key < out[j].Key
				}
				return out[i].ContactID < out[j].ContactID
			}
			return out[i].ObservedAt.After(out[j].ObservedAt)
		}
		return ri > rj
	})
	return out
}

func mapInferences(items map[string]MemoryInference) []MemoryInference {
	out := make([]MemoryInference, 0, len(items))
	for _, item := range items {
		out = append(out, item)
	}
	sort.SliceStable(out, func(i, j int) bool {
		ri := memoryFactRank(out[i].toFact())
		rj := memoryFactRank(out[j].toFact())
		if ri == rj {
			if out[i].ObservedAt.Equal(out[j].ObservedAt) {
				return out[i].ID < out[j].ID
			}
			return out[i].ObservedAt.After(out[j].ObservedAt)
		}
		return ri > rj
	})
	return out
}

func (m *AssistantMemory) observationByID(id string) (MemoryObservation, bool) {
	id = strings.TrimSpace(id)
	if id == "" {
		return MemoryObservation{}, false
	}
	for _, observation := range m.ObservationItems {
		if observation.ID == id {
			return observation, true
		}
	}
	return MemoryObservation{}, false
}

func (m *AssistantMemory) factByID(id string) (MemoryFact, bool) {
	id = strings.TrimSpace(id)
	if id == "" {
		return MemoryFact{}, false
	}
	for _, fact := range m.FactItems {
		if fact.ID == id {
			return fact, true
		}
	}
	return MemoryFact{}, false
}

func (m *AssistantMemory) inferenceByID(id string) (MemoryInference, bool) {
	id = strings.TrimSpace(id)
	if id == "" {
		return MemoryInference{}, false
	}
	for _, inference := range m.InferenceItems {
		if inference.ID == id {
			return inference, true
		}
	}
	return MemoryInference{}, false
}

func (m *AssistantMemory) UnmarshalJSON(data []byte) error {
	var raw struct {
		SchemaVersion    int                         `json:"schemaVersion,omitempty"`
		Owner            string                      `json:"owner,omitempty"`
		ContactsByID     map[string]AssistantContact `json:"contactsByID,omitempty"`
		ObservationItems []MemoryObservation         `json:"observationItems,omitempty"`
		Observations     []MemoryObservation         `json:"observations,omitempty"`
		InferenceItems   []MemoryInference           `json:"inferences,omitempty"`
		Inferences       []MemoryInference           `json:"inferenceItems,omitempty"`
		FactItems        []MemoryFact                `json:"factItems,omitempty"`
		Facts            []MemoryFact                `json:"facts,omitempty"`
		UpdatedAt        time.Time                   `json:"updatedAt,omitempty"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	m.SchemaVersion = raw.SchemaVersion
	m.Owner = strings.TrimSpace(raw.Owner)
	m.ContactsByID = raw.ContactsByID
	if len(raw.ObservationItems) > 0 {
		m.ObservationItems = raw.ObservationItems
	} else {
		m.ObservationItems = raw.Observations
	}
	if len(raw.InferenceItems) > 0 {
		m.InferenceItems = raw.InferenceItems
	} else {
		m.InferenceItems = raw.Inferences
	}
	if len(raw.FactItems) > 0 {
		m.FactItems = raw.FactItems
	} else {
		m.FactItems = raw.Facts
	}
	m.UpdatedAt = raw.UpdatedAt
	return nil
}
