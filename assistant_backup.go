package main

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
