package main

import (
	"archive/zip"
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type BackupCapability struct {
	Config AssistantConfig
}

type AssistantJournalBackup struct {
	Path         string    `json:"path"`
	Filename     string    `json:"filename"`
	EntryCount   int       `json:"entryCount"`
	Oldest       time.Time `json:"oldest,omitempty"`
	Newest       time.Time `json:"newest,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
	ManifestPath string    `json:"manifestPath,omitempty"`
}

type AssistantJournalImport struct {
	ArchivePath    string    `json:"archivePath"`
	ImportedCount  int       `json:"importedCount"`
	DuplicateCount int       `json:"duplicateCount"`
	TotalCount     int       `json:"totalCount"`
	Oldest         time.Time `json:"oldest,omitempty"`
	Newest         time.Time `json:"newest,omitempty"`
	Merged         bool      `json:"merged"`
}

type assistantJournalBackupManifest struct {
	Version      int       `json:"version"`
	CreatedAt    time.Time `json:"createdAt"`
	EntryCount   int       `json:"entryCount"`
	Oldest       time.Time `json:"oldest,omitempty"`
	Newest       time.Time `json:"newest,omitempty"`
	Files        []string  `json:"files,omitempty"`
	JournalDir   string    `json:"journalDir,omitempty"`
	JournalJSONL string    `json:"journalJsonl,omitempty"`
}

func (b *BackupCapability) Name() string { return "backup" }

func (b *BackupCapability) Description() string {
	return "Create portable Jot journal backup archives for migration and safekeeping."
}

func (b *BackupCapability) Tools() []Tool {
	return []Tool{
		{Name: "backup.export_journal", Description: "Create a portable archive of the local Jot journal for backup or migration.", ParamSchema: `{"type":"object","properties":{"output_dir":{"type":"string"},"filename":{"type":"string"}}}`},
		{Name: "backup.import_journal", Description: "Import a portable Jot journal archive into the local journal store.", ParamSchema: `{"type":"object","properties":{"archive_path":{"type":"string"},"merge":{"type":"boolean"}},"required":["archive_path"]}`},
		{Name: "backup.import_from_gmail", Description: "Find the latest emailed Jot journal backup in Gmail, download it, and import it into the local journal automatically.", ParamSchema: `{"type":"object","properties":{}}`},
	}
}

func (b *BackupCapability) Execute(toolName string, params map[string]any) (ToolResult, error) {
	switch strings.ToLower(strings.TrimSpace(toolName)) {
	case "backup.export_journal":
		backup, err := createJournalBackup(b.Config, strings.TrimSpace(firstStringParam(params, "output_dir", "save_dir")), strings.TrimSpace(firstStringParam(params, "filename")))
		if err != nil {
			return ToolResult{Success: false, Error: err.Error()}, err
		}
		return ToolResult{
			Success: true,
			Text:    fmt.Sprintf("journal backup created at %s", backup.Path),
			Data:    backup,
		}, nil
	case "backup.import_journal":
		imported, err := importJournalBackup(b.Config, strings.TrimSpace(firstStringParam(params, "archive_path", "path", "file")), paramBool(params, "merge"))
		if err != nil {
			return ToolResult{Success: false, Error: err.Error()}, err
		}
		return ToolResult{
			Success: true,
			Text:    fmt.Sprintf("journal imported from %s", imported.ArchivePath),
			Data:    imported,
		}, nil
	case "backup.import_from_gmail":
		err := errors.New("backup.import_from_gmail is handled by the assistant runtime")
		return ToolResult{Success: false, Error: err.Error()}, err
	default:
		err := fmt.Errorf("unknown backup tool %q", toolName)
		return ToolResult{Success: false, Error: err.Error()}, err
	}
}

func createJournalBackup(cfg AssistantConfig, outputDir, filename string) (AssistantJournalBackup, error) {
	journalPath, err := ensureJournalJSONL()
	if err != nil {
		return AssistantJournalBackup{}, err
	}
	entries, err := loadJournalEntries(journalPath)
	if err != nil {
		return AssistantJournalBackup{}, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return AssistantJournalBackup{}, err
	}
	journalDir, journalTxtPath, journalJSONLPath := journalPaths(home)
	if outputDir == "" {
		base := strings.TrimSpace(cfg.AttachmentSaveDir)
		if base == "" {
			base = filepath.Join(filepath.Dir(journalPath), "exports")
		}
		outputDir = filepath.Join(base, "journal-backups")
	}
	if err := os.MkdirAll(outputDir, 0o700); err != nil {
		return AssistantJournalBackup{}, err
	}
	createdAt := time.Now().UTC()
	if filename == "" {
		filename = fmt.Sprintf("jot-journal-backup-%s.zip", createdAt.Format("2006-01-02"))
	}
	if filepath.Ext(filename) == "" {
		filename += ".zip"
	}
	backupPath := filepath.Join(outputDir, filename)
	file, err := os.OpenFile(backupPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return AssistantJournalBackup{}, err
	}
	defer file.Close()

	zipWriter := zip.NewWriter(file)
	files := []string{}
	if err := addBackupFile(zipWriter, journalJSONLPath, "journal.jsonl"); err != nil {
		zipWriter.Close()
		return AssistantJournalBackup{}, err
	}
	files = append(files, "journal.jsonl")
	if _, err := os.Stat(journalTxtPath); err == nil {
		if err := addBackupFile(zipWriter, journalTxtPath, "journal.txt"); err != nil {
			zipWriter.Close()
			return AssistantJournalBackup{}, err
		}
		files = append(files, "journal.txt")
	}
	manifest := assistantJournalBackupManifest{
		Version:      1,
		CreatedAt:    createdAt,
		EntryCount:   len(entries),
		Oldest:       journalOldest(entries),
		Newest:       journalNewest(entries),
		Files:        files,
		JournalDir:   journalDir,
		JournalJSONL: journalJSONLPath,
	}
	if err := addBackupManifest(zipWriter, manifest); err != nil {
		zipWriter.Close()
		return AssistantJournalBackup{}, err
	}
	if err := zipWriter.Close(); err != nil {
		return AssistantJournalBackup{}, err
	}
	return AssistantJournalBackup{
		Path:         backupPath,
		Filename:     filename,
		EntryCount:   len(entries),
		Oldest:       manifest.Oldest,
		Newest:       manifest.Newest,
		CreatedAt:    createdAt,
		ManifestPath: "manifest.json",
	}, nil
}

func addBackupFile(zipWriter *zip.Writer, sourcePath, archiveName string) error {
	data, err := os.ReadFile(sourcePath)
	if err != nil {
		return err
	}
	writer, err := zipWriter.Create(archiveName)
	if err != nil {
		return err
	}
	_, err = writer.Write(data)
	return err
}

func addBackupManifest(zipWriter *zip.Writer, manifest assistantJournalBackupManifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	writer, err := zipWriter.Create("manifest.json")
	if err != nil {
		return err
	}
	_, err = writer.Write(data)
	return err
}

func journalOldest(entries []journalEntry) time.Time {
	if len(entries) == 0 {
		return time.Time{}
	}
	oldest := entries[0].CreatedAt
	for _, entry := range entries[1:] {
		if entry.CreatedAt.Before(oldest) {
			oldest = entry.CreatedAt
		}
	}
	return oldest
}

func journalNewest(entries []journalEntry) time.Time {
	if len(entries) == 0 {
		return time.Time{}
	}
	newest := entries[0].CreatedAt
	for _, entry := range entries[1:] {
		if entry.CreatedAt.After(newest) {
			newest = entry.CreatedAt
		}
	}
	return newest
}

func resolveAttachmentSendPaths(params map[string]any) ([]string, error) {
	values := append(paramStringSlice(params, "attachment_paths", "attachments", "files"), strings.TrimSpace(firstStringParam(params, "attachment_path", "file")))
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		abs, err := filepath.Abs(value)
		if err != nil {
			return nil, err
		}
		info, err := os.Stat(abs)
		if err != nil {
			return nil, err
		}
		if info.IsDir() {
			return nil, errors.New("attachment path must be a file")
		}
		if _, ok := seen[strings.ToLower(abs)]; ok {
			continue
		}
		seen[strings.ToLower(abs)] = struct{}{}
		out = append(out, abs)
	}
	return out, nil
}

func importJournalBackup(cfg AssistantConfig, archivePath string, merge bool) (AssistantJournalImport, error) {
	archivePath = strings.TrimSpace(archivePath)
	if archivePath == "" {
		return AssistantJournalImport{}, errors.New("archive_path must be provided")
	}
	absArchive, err := filepath.Abs(archivePath)
	if err != nil {
		return AssistantJournalImport{}, err
	}
	reader, err := zip.OpenReader(absArchive)
	if err != nil {
		return AssistantJournalImport{}, err
	}
	defer reader.Close()

	var importedEntries []journalEntry
	foundJournalJSONL := false
	for _, file := range reader.File {
		switch file.Name {
		case "journal.jsonl":
			foundJournalJSONL = true
			rc, err := file.Open()
			if err != nil {
				return AssistantJournalImport{}, err
			}
			data, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return AssistantJournalImport{}, err
			}
			importedEntries, err = loadJournalEntriesFromReader(strings.NewReader(string(data)))
			if err != nil {
				return AssistantJournalImport{}, err
			}
		case "journal.txt":
			if foundJournalJSONL {
				continue
			}
			rc, err := file.Open()
			if err != nil {
				return AssistantJournalImport{}, err
			}
			items, err := collectJournalEntries(rc, file.Name)
			rc.Close()
			if err != nil {
				return AssistantJournalImport{}, err
			}
			importedEntries = make([]journalEntry, 0, len(items))
			for i, item := range items {
				importedEntries = append(importedEntries, journalEntryFromListItem(item, i))
			}
		}
	}
	if len(importedEntries) == 0 {
		return AssistantJournalImport{}, errors.New("backup archive does not contain any journal entries")
	}

	journalPath, err := ensureJournalJSONL()
	if err != nil {
		return AssistantJournalImport{}, err
	}
	existingEntries, err := loadJournalEntries(journalPath)
	if err != nil {
		return AssistantJournalImport{}, err
	}
	existingByID := make(map[string]journalEntry, len(existingEntries))
	for _, entry := range existingEntries {
		if strings.TrimSpace(entry.ID) == "" {
			continue
		}
		existingByID[strings.TrimSpace(entry.ID)] = entry
	}

	if !merge {
		existingByID = map[string]journalEntry{}
	}
	importedCount := 0
	duplicateCount := 0
	for _, entry := range importedEntries {
		id := strings.TrimSpace(entry.ID)
		if id == "" {
			entry.ID = newEntryID(entry.CreatedAt, importedCount+duplicateCount)
			id = entry.ID
		}
		if _, ok := existingByID[id]; ok {
			duplicateCount++
			continue
		}
		existingByID[id] = entry
		importedCount++
	}

	mergedEntries := make([]journalEntry, 0, len(existingByID))
	for _, entry := range existingByID {
		mergedEntries = append(mergedEntries, entry)
	}
	sort.SliceStable(mergedEntries, func(i, j int) bool {
		if mergedEntries[i].CreatedAt.Equal(mergedEntries[j].CreatedAt) {
			return strings.TrimSpace(mergedEntries[i].ID) < strings.TrimSpace(mergedEntries[j].ID)
		}
		return mergedEntries[i].CreatedAt.Before(mergedEntries[j].CreatedAt)
	})

	if err := writeJournalEntries(journalPath, mergedEntries); err != nil {
		return AssistantJournalImport{}, err
	}

	return AssistantJournalImport{
		ArchivePath:    absArchive,
		ImportedCount:  importedCount,
		DuplicateCount: duplicateCount,
		TotalCount:     len(mergedEntries),
		Oldest:         journalOldest(mergedEntries),
		Newest:         journalNewest(mergedEntries),
		Merged:         merge,
	}, nil
}

func loadJournalEntriesFromReader(r io.Reader) ([]journalEntry, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	var entries []journalEntry
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var entry journalEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			return nil, err
		}
		if entry.CreatedAt.IsZero() {
			entry.CreatedAt = time.Now()
		}
		if strings.TrimSpace(entry.ID) == "" {
			entry.ID = newEntryID(entry.CreatedAt, len(entries))
		}
		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}

func writeJournalEntries(path string, entries []journalEntry) error {
	tmpPath := path + ".tmp"
	file, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(file)
	encoder.SetEscapeHTML(false)
	for _, entry := range entries {
		if err := encoder.Encode(entry); err != nil {
			file.Close()
			_ = os.Remove(tmpPath)
			return err
		}
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, path)
}
