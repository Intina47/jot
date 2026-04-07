package main

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const assistantFeedFileName = "assistant_feed.json"
const assistantFeedSchemaVersion = 1

type AssistantFeedStatus string
type AssistantFeedKind string

const (
	AssistantFeedStatusNew       AssistantFeedStatus = "new"
	AssistantFeedStatusSeen      AssistantFeedStatus = "seen"
	AssistantFeedStatusSnoozed   AssistantFeedStatus = "snoozed"
	AssistantFeedStatusDone      AssistantFeedStatus = "done"
	AssistantFeedStatusDismissed AssistantFeedStatus = "dismissed"
	AssistantFeedStatusExpired   AssistantFeedStatus = "expired"

	AssistantFeedKindNote           AssistantFeedKind = "note"
	AssistantFeedKindDraftReply     AssistantFeedKind = "draft_reply"
	AssistantFeedKindDeadlineAlert  AssistantFeedKind = "deadline_alert"
	AssistantFeedKindTripSuggestion AssistantFeedKind = "trip_suggestion"
	AssistantFeedKindGiftSuggestion AssistantFeedKind = "gift_suggestion"
	AssistantFeedKindPrepPlan       AssistantFeedKind = "prep_plan"
	AssistantFeedKindResearchBrief  AssistantFeedKind = "research_brief"
	AssistantFeedKindFollowUpNeeded AssistantFeedKind = "follow_up_needed"
)

type AssistantFeedLink struct {
	Label   string `json:"label,omitempty"`
	URL     string `json:"url,omitempty"`
	Preview string `json:"preview,omitempty"`
}

type AssistantFeedItem struct {
	ID               string              `json:"id"`
	Key              string              `json:"key,omitempty"`
	Kind             AssistantFeedKind   `json:"kind,omitempty"`
	Status           AssistantFeedStatus `json:"status,omitempty"`
	Eyebrow          string              `json:"eyebrow,omitempty"`
	Title            string              `json:"title,omitempty"`
	Summary          string              `json:"summary,omitempty"`
	Body             string              `json:"body,omitempty"`
	Note             string              `json:"note,omitempty"`
	Reason           string              `json:"reason,omitempty"`
	Evidence         string              `json:"evidence,omitempty"`
	SourceType       string              `json:"sourceType,omitempty"`
	SourceID         string              `json:"sourceId,omitempty"`
	SourceRefs       []string            `json:"sourceRefs,omitempty"`
	MemoryRefs       []string            `json:"memoryRefs,omitempty"`
	RelatedMemoryIDs []string            `json:"relatedMemoryIds,omitempty"`
	Links            []AssistantFeedLink `json:"links,omitempty"`
	Confidence       MemoryConfidence    `json:"confidence,omitempty"`
	Importance       int                 `json:"importance,omitempty"`
	CreatedAt        time.Time           `json:"createdAt,omitempty"`
	UpdatedAt        time.Time           `json:"updatedAt,omitempty"`
	SeenAt           time.Time           `json:"seenAt,omitempty"`
	SnoozedUntil     time.Time           `json:"snoozedUntil,omitempty"`
	DueAt            time.Time           `json:"dueAt,omitempty"`
	ExpiresAt        time.Time           `json:"expiresAt,omitempty"`
	ClosedAt         time.Time           `json:"closedAt,omitempty"`
	RetrievalText    string              `json:"retrievalText,omitempty"`
}

type AssistantFeed struct {
	path string
	mu   sync.Mutex

	SchemaVersion int                 `json:"schemaVersion,omitempty"`
	Items         []AssistantFeedItem `json:"items,omitempty"`
	UpdatedAt     time.Time           `json:"updatedAt,omitempty"`
}

func NewAssistantFeedAt(path string) *AssistantFeed {
	feed := &AssistantFeed{path: strings.TrimSpace(path)}
	feed.normalize(time.Now().UTC())
	return feed
}

func LoadAssistantFeed(cfg AssistantConfig) (*AssistantFeed, error) {
	return LoadAssistantFeedAt(cfg.FeedPath)
}

func assistantFeedPath(cfg AssistantConfig) string {
	if path := strings.TrimSpace(cfg.FeedPath); path != "" {
		return path
	}
	return filepath.Join(assistantConfigDirOrFallback(), assistantFeedFileName)
}

func assistantConfigDirOrFallback() string {
	if dir, err := assistantConfigDir(); err == nil && strings.TrimSpace(dir) != "" {
		return dir
	}
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		return filepath.Join(home, ".config", "jot")
	}
	return "."
}

func LoadAssistantFeedAt(path string) (*AssistantFeed, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return NewAssistantFeedAt(""), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return NewAssistantFeedAt(path), nil
		}
		return nil, err
	}
	feed := &AssistantFeed{path: path}
	if err := json.Unmarshal(trimUTF8BOM(data), feed); err != nil {
		return nil, err
	}
	originalSchemaVersion := feed.SchemaVersion
	if feed.normalize(time.Now().UTC()) || originalSchemaVersion < assistantFeedSchemaVersion {
		if err := feed.Save(path); err != nil {
			return nil, err
		}
	}
	return feed, nil
}

func (f *AssistantFeed) Save(path string) error {
	if f == nil {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if strings.TrimSpace(path) == "" {
		path = f.path
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	f.normalizeLocked(time.Now().UTC())
	f.path = path
	return writeSecureJSON(path, f)
}

func (f *AssistantFeed) AddItem(item AssistantFeedItem) (AssistantFeedItem, error) {
	if f == nil {
		return AssistantFeedItem{}, nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	now := time.Now().UTC()
	f.normalizeLocked(now)
	item = f.normalizeItem(item, now)

	lookupID := item.Key
	if strings.TrimSpace(lookupID) == "" {
		lookupID = item.ID
	}
	if idx, ok := f.findIndexLocked(lookupID); ok {
		merged := assistantFeedMergeItem(f.Items[idx], item)
		merged = f.normalizeItem(merged, now)
		f.Items[idx] = merged
		f.normalizeLocked(now)
		if strings.TrimSpace(f.path) != "" {
			if err := writeSecureJSON(f.path, f); err != nil {
				return AssistantFeedItem{}, err
			}
		}
		return merged, nil
	}

	f.Items = append(f.Items, item)
	f.normalizeLocked(now)
	if strings.TrimSpace(f.path) != "" {
		if err := writeSecureJSON(f.path, f); err != nil {
			return AssistantFeedItem{}, err
		}
	}
	stored, ok := f.findItemLocked(item.ID)
	if ok {
		return stored, nil
	}
	return item, nil
}

func (f *AssistantFeed) Upsert(item AssistantFeedItem) (AssistantFeedItem, error) {
	return f.AddItem(item)
}

func (f *AssistantFeed) UpdateItem(id string, patch AssistantFeedItem) (AssistantFeedItem, bool, error) {
	if f == nil {
		return AssistantFeedItem{}, false, nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	now := time.Now().UTC()
	f.normalizeLocked(now)
	idx, ok := f.findIndexLocked(id)
	if !ok {
		return AssistantFeedItem{}, false, nil
	}
	current := f.Items[idx]
	updated := assistantFeedPatchItem(current, patch)
	updated = f.normalizeItem(updated, now)
	f.Items[idx] = updated
	f.normalizeLocked(now)
	if strings.TrimSpace(f.path) != "" {
		if err := writeSecureJSON(f.path, f); err != nil {
			return AssistantFeedItem{}, false, err
		}
	}
	return updated, true, nil
}

func (f *AssistantFeed) MarkSeen(id string, at time.Time) (AssistantFeedItem, bool, error) {
	return f.UpdateItem(id, AssistantFeedItem{
		Status: AssistantFeedStatusSeen,
		SeenAt: at,
	})
}

func (f *AssistantFeed) Snooze(id string, until time.Time, at time.Time) (AssistantFeedItem, bool, error) {
	return f.UpdateItem(id, AssistantFeedItem{
		Status:       AssistantFeedStatusSnoozed,
		SnoozedUntil: until,
		UpdatedAt:    at,
	})
}

func (f *AssistantFeed) MarkDone(id string, at time.Time) (AssistantFeedItem, bool, error) {
	return f.UpdateItem(id, AssistantFeedItem{
		Status:    AssistantFeedStatusDone,
		ClosedAt:  at,
		UpdatedAt: at,
	})
}

func (f *AssistantFeed) Dismiss(id string, at time.Time) (AssistantFeedItem, bool, error) {
	return f.UpdateItem(id, AssistantFeedItem{
		Status:    AssistantFeedStatusDismissed,
		ClosedAt:  at,
		UpdatedAt: at,
	})
}

func (f *AssistantFeed) Reopen(id string, at time.Time) (AssistantFeedItem, bool, error) {
	if f == nil {
		return AssistantFeedItem{}, false, nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	now := time.Now().UTC()
	f.normalizeLocked(now)
	idx, ok := f.findIndexLocked(id)
	if !ok {
		return AssistantFeedItem{}, false, nil
	}
	item := f.Items[idx]
	item.Status = AssistantFeedStatusSeen
	item.ClosedAt = time.Time{}
	item.UpdatedAt = at.UTC()
	item.SeenAt = at.UTC()
	item = f.normalizeItem(item, now)
	f.Items[idx] = item
	f.normalizeLocked(now)
	if strings.TrimSpace(f.path) != "" {
		if err := writeSecureJSON(f.path, f); err != nil {
			return AssistantFeedItem{}, false, err
		}
	}
	return item, true, nil
}

func (f *AssistantFeed) ApplyAction(actionID string, at time.Time) (AssistantFeedItem, bool, error) {
	if f == nil {
		return AssistantFeedItem{}, false, nil
	}
	actionID = strings.TrimSpace(actionID)
	if !strings.HasPrefix(actionID, "feed:") {
		return AssistantFeedItem{}, false, nil
	}
	rest := strings.TrimPrefix(actionID, "feed:")
	itemID, action, ok := strings.Cut(rest, ":")
	if !ok {
		return AssistantFeedItem{}, false, nil
	}
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "done":
		return f.MarkDone(itemID, at)
	case "dismiss":
		return f.Dismiss(itemID, at)
	case "snooze":
		return f.Snooze(itemID, at.Add(24*time.Hour), at)
	case "resume", "reopen", "seen":
		return f.Reopen(itemID, at)
	default:
		return AssistantFeedItem{}, false, nil
	}
}

func (f *AssistantFeed) ItemsList(now time.Time) []AssistantFeedItem {
	if f == nil {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	changed := f.normalizeLocked(now.UTC())
	if changed && strings.TrimSpace(f.path) != "" {
		_ = writeSecureJSON(f.path, f)
	}
	out := make([]AssistantFeedItem, len(f.Items))
	copy(out, f.Items)
	sort.SliceStable(out, func(i, j int) bool {
		return assistantFeedCompareItems(out[i], out[j], now)
	})
	return out
}

func (f *AssistantFeed) VisibleItems(now time.Time) []AssistantFeedItem {
	items := f.ItemsList(now)
	if len(items) == 0 {
		return nil
	}
	out := make([]AssistantFeedItem, 0, len(items))
	for _, item := range items {
		if assistantFeedIsVisible(item) {
			out = append(out, item)
		}
	}
	return out
}

func (f *AssistantFeed) findIndexLocked(id string) (int, bool) {
	id = strings.TrimSpace(id)
	if id == "" {
		return -1, false
	}
	for idx, item := range f.Items {
		if item.ID == id || item.Key == id {
			return idx, true
		}
	}
	return -1, false
}

func (f *AssistantFeed) findItemLocked(id string) (AssistantFeedItem, bool) {
	if idx, ok := f.findIndexLocked(id); ok {
		return f.Items[idx], true
	}
	return AssistantFeedItem{}, false
}

func (f *AssistantFeed) normalize(now time.Time) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.normalizeLocked(now.UTC())
}

func (f *AssistantFeed) normalizeLocked(now time.Time) bool {
	changed := false
	if f.SchemaVersion != assistantFeedSchemaVersion {
		f.SchemaVersion = assistantFeedSchemaVersion
		changed = true
	}
	now = now.UTC()
	for idx, item := range f.Items {
		next, itemChanged := f.normalizeItemWithChange(item, now)
		if itemChanged {
			changed = true
		}
		f.Items[idx] = next
	}
	if len(f.Items) > 1 {
		sort.SliceStable(f.Items, func(i, j int) bool {
			return assistantFeedCompareItems(f.Items[i], f.Items[j], now)
		})
	}
	if changed {
		f.UpdatedAt = now
	}
	return changed
}

func (f *AssistantFeed) normalizeItem(item AssistantFeedItem, now time.Time) AssistantFeedItem {
	item, _ = f.normalizeItemWithChange(item, now)
	return item
}

func (f *AssistantFeed) normalizeItemWithChange(item AssistantFeedItem, now time.Time) (AssistantFeedItem, bool) {
	changed := false
	now = now.UTC()

	item.Key = strings.TrimSpace(item.Key)
	item.Eyebrow = strings.TrimSpace(item.Eyebrow)
	item.Title = strings.TrimSpace(item.Title)
	item.Summary = strings.TrimSpace(item.Summary)
	item.Body = strings.TrimSpace(item.Body)
	item.Note = strings.TrimSpace(item.Note)
	item.Reason = strings.TrimSpace(item.Reason)
	item.Evidence = strings.TrimSpace(item.Evidence)
	item.SourceType = strings.ToLower(strings.TrimSpace(item.SourceType))
	item.SourceID = strings.TrimSpace(item.SourceID)
	item.SourceRefs = assistantNormalizeStringList(item.SourceRefs)
	item.MemoryRefs = assistantNormalizeStringList(item.MemoryRefs)
	item.RelatedMemoryIDs = assistantNormalizeStringList(item.RelatedMemoryIDs)
	item.Confidence = normalizeConfidence(item.Confidence)
	item.Kind = assistantFeedNormalizeKind(item.Kind)
	item.Status = assistantFeedNormalizeStatus(item.Status)
	item.RetrievalText = strings.TrimSpace(item.RetrievalText)

	for idx := range item.Links {
		item.Links[idx].Label = strings.TrimSpace(item.Links[idx].Label)
		item.Links[idx].URL = strings.TrimSpace(item.Links[idx].URL)
		item.Links[idx].Preview = strings.TrimSpace(item.Links[idx].Preview)
	}
	item.Links = assistantFeedNormalizeLinks(item.Links)

	if item.ID == "" {
		item.ID = assistantFeedFingerprint(item)
		changed = true
	}
	if item.Key == "" {
		item.Key = item.ID
		changed = true
	}
	if item.CreatedAt.IsZero() {
		item.CreatedAt = now
		changed = true
	}
	if item.UpdatedAt.IsZero() {
		item.UpdatedAt = item.CreatedAt
		changed = true
	}
	if item.Status == "" {
		item.Status = AssistantFeedStatusNew
		changed = true
	}

	if !item.SnoozedUntil.IsZero() {
		item.SnoozedUntil = item.SnoozedUntil.UTC()
	}
	if !item.DueAt.IsZero() {
		item.DueAt = item.DueAt.UTC()
	}
	if !item.ExpiresAt.IsZero() {
		item.ExpiresAt = item.ExpiresAt.UTC()
	}
	if !item.SeenAt.IsZero() {
		item.SeenAt = item.SeenAt.UTC()
	}
	if !item.ClosedAt.IsZero() {
		item.ClosedAt = item.ClosedAt.UTC()
	}

	if item.Importance < 0 {
		item.Importance = 0
		changed = true
	}
	if item.Importance > 100 {
		item.Importance = 100
		changed = true
	}

	if item.Status == AssistantFeedStatusSnoozed && !item.SnoozedUntil.IsZero() && !now.Before(item.SnoozedUntil) {
		item.Status = AssistantFeedStatusNew
		item.SnoozedUntil = time.Time{}
		changed = true
	}
	if !item.ExpiresAt.IsZero() && !now.Before(item.ExpiresAt) && item.Status != AssistantFeedStatusDone && item.Status != AssistantFeedStatusDismissed {
		item.Status = AssistantFeedStatusExpired
		if item.ClosedAt.IsZero() {
			item.ClosedAt = now
		}
		changed = true
	}

	switch item.Status {
	case AssistantFeedStatusDone, AssistantFeedStatusDismissed, AssistantFeedStatusExpired:
		if item.ClosedAt.IsZero() {
			item.ClosedAt = now
			changed = true
		}
	}

	retrievalText := assistantFeedBuildRetrievalText(item)
	if item.RetrievalText != retrievalText {
		item.RetrievalText = retrievalText
		changed = true
	}
	return item, changed
}

func assistantFeedPatchItem(existing, patch AssistantFeedItem) AssistantFeedItem {
	if strings.TrimSpace(patch.ID) != "" && patch.ID != existing.ID {
		existing.ID = strings.TrimSpace(patch.ID)
	}
	if strings.TrimSpace(patch.Key) != "" {
		existing.Key = strings.TrimSpace(patch.Key)
	}
	if strings.TrimSpace(string(patch.Kind)) != "" {
		existing.Kind = patch.Kind
	}
	if strings.TrimSpace(string(patch.Status)) != "" {
		existing.Status = patch.Status
	}
	if strings.TrimSpace(patch.Eyebrow) != "" {
		existing.Eyebrow = patch.Eyebrow
	}
	if strings.TrimSpace(patch.Title) != "" {
		existing.Title = patch.Title
	}
	if strings.TrimSpace(patch.Summary) != "" {
		existing.Summary = patch.Summary
	}
	if strings.TrimSpace(patch.Body) != "" {
		existing.Body = patch.Body
	}
	if strings.TrimSpace(patch.Note) != "" {
		existing.Note = patch.Note
	}
	if strings.TrimSpace(patch.Reason) != "" {
		existing.Reason = patch.Reason
	}
	if strings.TrimSpace(patch.Evidence) != "" {
		existing.Evidence = patch.Evidence
	}
	if strings.TrimSpace(patch.SourceType) != "" {
		existing.SourceType = patch.SourceType
	}
	if strings.TrimSpace(patch.SourceID) != "" {
		existing.SourceID = patch.SourceID
	}
	if len(patch.SourceRefs) > 0 {
		existing.SourceRefs = assistantNormalizeStringList(append(existing.SourceRefs, patch.SourceRefs...))
	}
	if len(patch.MemoryRefs) > 0 {
		existing.MemoryRefs = assistantNormalizeStringList(append(existing.MemoryRefs, patch.MemoryRefs...))
	}
	if len(patch.RelatedMemoryIDs) > 0 {
		existing.RelatedMemoryIDs = assistantNormalizeStringList(append(existing.RelatedMemoryIDs, patch.RelatedMemoryIDs...))
	}
	if len(patch.Links) > 0 {
		existing.Links = assistantFeedNormalizeLinks(append(existing.Links, patch.Links...))
	}
	if strings.TrimSpace(string(patch.Confidence)) != "" {
		existing.Confidence = patch.Confidence
	}
	if patch.Importance != 0 {
		existing.Importance = patch.Importance
	}
	if !patch.CreatedAt.IsZero() {
		existing.CreatedAt = patch.CreatedAt
	}
	if !patch.UpdatedAt.IsZero() {
		existing.UpdatedAt = patch.UpdatedAt
	}
	if !patch.SeenAt.IsZero() {
		existing.SeenAt = patch.SeenAt
	}
	if !patch.SnoozedUntil.IsZero() {
		existing.SnoozedUntil = patch.SnoozedUntil
	}
	if !patch.DueAt.IsZero() {
		existing.DueAt = patch.DueAt
	}
	if !patch.ExpiresAt.IsZero() {
		existing.ExpiresAt = patch.ExpiresAt
	}
	if !patch.ClosedAt.IsZero() {
		existing.ClosedAt = patch.ClosedAt
	}
	if strings.TrimSpace(patch.RetrievalText) != "" {
		existing.RetrievalText = patch.RetrievalText
	}
	return assistantFeedMergeItem(existing, patch)
}

func assistantFeedMergeItem(existing, next AssistantFeedItem) AssistantFeedItem {
	if next.Key != "" {
		existing.Key = next.Key
	}
	if next.Kind != "" {
		existing.Kind = next.Kind
	}
	if next.Eyebrow != "" {
		existing.Eyebrow = next.Eyebrow
	}
	if next.Title != "" {
		existing.Title = next.Title
	}
	if next.Summary != "" {
		existing.Summary = next.Summary
	}
	if next.Body != "" {
		existing.Body = next.Body
	}
	if next.Note != "" {
		existing.Note = next.Note
	}
	if next.Reason != "" {
		existing.Reason = next.Reason
	}
	if next.Evidence != "" {
		existing.Evidence = next.Evidence
	}
	if next.SourceType != "" {
		existing.SourceType = next.SourceType
	}
	if next.SourceID != "" {
		existing.SourceID = next.SourceID
	}
	if len(next.SourceRefs) > 0 {
		existing.SourceRefs = assistantNormalizeStringList(append(existing.SourceRefs, next.SourceRefs...))
	}
	if len(next.MemoryRefs) > 0 {
		existing.MemoryRefs = assistantNormalizeStringList(append(existing.MemoryRefs, next.MemoryRefs...))
	}
	if len(next.RelatedMemoryIDs) > 0 {
		existing.RelatedMemoryIDs = assistantNormalizeStringList(append(existing.RelatedMemoryIDs, next.RelatedMemoryIDs...))
	}
	if len(next.Links) > 0 {
		existing.Links = assistantFeedNormalizeLinks(append(existing.Links, next.Links...))
	}
	if next.Confidence != "" {
		if assistantFeedConfidenceRank(next.Confidence) >= assistantFeedConfidenceRank(existing.Confidence) {
			existing.Confidence = next.Confidence
		}
	}
	if next.Importance > existing.Importance {
		existing.Importance = next.Importance
	}
	if !next.CreatedAt.IsZero() && (existing.CreatedAt.IsZero() || next.CreatedAt.Before(existing.CreatedAt)) {
		existing.CreatedAt = next.CreatedAt
	}
	if !next.UpdatedAt.IsZero() && next.UpdatedAt.After(existing.UpdatedAt) {
		existing.UpdatedAt = next.UpdatedAt
	}
	if !next.SeenAt.IsZero() && next.SeenAt.After(existing.SeenAt) {
		existing.SeenAt = next.SeenAt
	}
	if !next.SnoozedUntil.IsZero() {
		existing.SnoozedUntil = next.SnoozedUntil
	}
	if !next.DueAt.IsZero() && (existing.DueAt.IsZero() || next.DueAt.Before(existing.DueAt)) {
		existing.DueAt = next.DueAt
	}
	if !next.ExpiresAt.IsZero() && (existing.ExpiresAt.IsZero() || next.ExpiresAt.After(existing.ExpiresAt)) {
		existing.ExpiresAt = next.ExpiresAt
	}
	if !next.ClosedAt.IsZero() && (existing.ClosedAt.IsZero() || next.ClosedAt.After(existing.ClosedAt)) {
		existing.ClosedAt = next.ClosedAt
	}
	if next.RetrievalText != "" {
		existing.RetrievalText = next.RetrievalText
	}
	if next.Status != "" {
		if assistantFeedStatusIsTerminal(next.Status) || !assistantFeedStatusIsTerminal(existing.Status) {
			if assistantFeedStatusRank(next.Status) >= assistantFeedStatusRank(existing.Status) || assistantFeedStatusIsTerminal(next.Status) {
				existing.Status = next.Status
			}
		}
	}
	return existing
}

func assistantFeedIsVisible(item AssistantFeedItem) bool {
	switch item.Status {
	case AssistantFeedStatusDone, AssistantFeedStatusDismissed, AssistantFeedStatusExpired:
		return false
	default:
		return true
	}
}

func assistantFeedCompareItems(left, right AssistantFeedItem, now time.Time) bool {
	leftRank := assistantFeedStatusRank(left.Status)
	rightRank := assistantFeedStatusRank(right.Status)
	if leftRank == rightRank {
		if left.Importance == right.Importance {
			leftDue := assistantFeedEffectiveDue(left)
			rightDue := assistantFeedEffectiveDue(right)
			if leftDue.Equal(rightDue) {
				if left.UpdatedAt.Equal(right.UpdatedAt) {
					return left.ID < right.ID
				}
				return left.UpdatedAt.After(right.UpdatedAt)
			}
			if leftDue.IsZero() {
				return false
			}
			if rightDue.IsZero() {
				return true
			}
			return leftDue.Before(rightDue)
		}
		return left.Importance > right.Importance
	}
	return leftRank > rightRank
}

func assistantFeedEffectiveDue(item AssistantFeedItem) time.Time {
	if !item.SnoozedUntil.IsZero() {
		return item.SnoozedUntil
	}
	return item.DueAt
}

func assistantFeedNormalizeStatus(value AssistantFeedStatus) AssistantFeedStatus {
	switch strings.ToLower(strings.TrimSpace(string(value))) {
	case string(AssistantFeedStatusSeen):
		return AssistantFeedStatusSeen
	case string(AssistantFeedStatusSnoozed):
		return AssistantFeedStatusSnoozed
	case string(AssistantFeedStatusDone):
		return AssistantFeedStatusDone
	case string(AssistantFeedStatusDismissed):
		return AssistantFeedStatusDismissed
	case string(AssistantFeedStatusExpired):
		return AssistantFeedStatusExpired
	default:
		return AssistantFeedStatusNew
	}
}

func assistantFeedNormalizeKind(value AssistantFeedKind) AssistantFeedKind {
	switch strings.ToLower(strings.TrimSpace(string(value))) {
	case string(AssistantFeedKindDraftReply):
		return AssistantFeedKindDraftReply
	case string(AssistantFeedKindDeadlineAlert):
		return AssistantFeedKindDeadlineAlert
	case string(AssistantFeedKindTripSuggestion):
		return AssistantFeedKindTripSuggestion
	case string(AssistantFeedKindGiftSuggestion):
		return AssistantFeedKindGiftSuggestion
	case string(AssistantFeedKindPrepPlan):
		return AssistantFeedKindPrepPlan
	case string(AssistantFeedKindResearchBrief):
		return AssistantFeedKindResearchBrief
	case string(AssistantFeedKindFollowUpNeeded):
		return AssistantFeedKindFollowUpNeeded
	default:
		return AssistantFeedKindNote
	}
}

func assistantFeedStatusRank(value AssistantFeedStatus) int {
	switch assistantFeedNormalizeStatus(value) {
	case AssistantFeedStatusNew:
		return 4
	case AssistantFeedStatusSeen:
		return 3
	case AssistantFeedStatusSnoozed:
		return 2
	case AssistantFeedStatusExpired:
		return 1
	case AssistantFeedStatusDone, AssistantFeedStatusDismissed:
		return 0
	default:
		return 0
	}
}

func assistantFeedStatusIsTerminal(value AssistantFeedStatus) bool {
	switch assistantFeedNormalizeStatus(value) {
	case AssistantFeedStatusDone, AssistantFeedStatusDismissed, AssistantFeedStatusExpired:
		return true
	default:
		return false
	}
}

func assistantFeedConfidenceRank(value MemoryConfidence) int {
	switch normalizeConfidence(value) {
	case MemoryConfidenceHigh:
		return 3
	case MemoryConfidenceMedium:
		return 2
	case MemoryConfidenceLow:
		return 1
	default:
		return 0
	}
}

func assistantFeedFingerprint(item AssistantFeedItem) string {
	var parts []string
	parts = append(parts,
		strings.ToLower(strings.TrimSpace(string(item.Kind))),
		strings.ToLower(strings.TrimSpace(item.SourceType)),
		strings.ToLower(strings.TrimSpace(item.SourceID)),
		strings.ToLower(strings.TrimSpace(item.Title)),
		strings.ToLower(strings.TrimSpace(item.Summary)),
		strings.ToLower(strings.TrimSpace(item.Reason)),
		assistantFeedTimeFingerprint(item.DueAt),
		assistantFeedTimeFingerprint(item.ExpiresAt),
	)
	sum := sha1.Sum([]byte(strings.Join(parts, "|")))
	return "feed-" + hex.EncodeToString(sum[:8])
}

func assistantFeedTimeFingerprint(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func assistantFeedNormalizeLinks(links []AssistantFeedLink) []AssistantFeedLink {
	if len(links) == 0 {
		return nil
	}
	out := make([]AssistantFeedLink, 0, len(links))
	seen := make(map[string]struct{}, len(links))
	for _, link := range links {
		label := strings.TrimSpace(link.Label)
		url := strings.TrimSpace(link.URL)
		preview := strings.TrimSpace(link.Preview)
		if label == "" && url == "" {
			continue
		}
		key := strings.ToLower(label + "\n" + url)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, AssistantFeedLink{Label: label, URL: url, Preview: preview})
	}
	return out
}

func assistantFeedBuildRetrievalText(item AssistantFeedItem) string {
	var parts []string
	if item.Title != "" {
		parts = append(parts, item.Title)
	}
	if item.Eyebrow != "" {
		parts = append(parts, item.Eyebrow)
	}
	if item.Summary != "" {
		parts = append(parts, item.Summary)
	}
	if item.Body != "" {
		parts = append(parts, item.Body)
	}
	if item.Note != "" {
		parts = append(parts, item.Note)
	}
	if item.Reason != "" {
		parts = append(parts, item.Reason)
	}
	if item.SourceType != "" {
		parts = append(parts, item.SourceType)
	}
	if item.SourceID != "" {
		parts = append(parts, item.SourceID)
	}
	for _, ref := range item.SourceRefs {
		if strings.TrimSpace(ref) != "" {
			parts = append(parts, ref)
		}
	}
	for _, ref := range item.MemoryRefs {
		if strings.TrimSpace(ref) != "" {
			parts = append(parts, ref)
		}
	}
	for _, ref := range item.RelatedMemoryIDs {
		if strings.TrimSpace(ref) != "" {
			parts = append(parts, ref)
		}
	}
	for _, link := range item.Links {
		if strings.TrimSpace(link.Label) != "" {
			parts = append(parts, link.Label)
		}
		if strings.TrimSpace(link.Preview) != "" {
			parts = append(parts, link.Preview)
		}
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}
