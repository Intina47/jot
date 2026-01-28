package main

import (
	"fmt"
	"path/filepath"
	"time"
)

func conflictCopyPath(originalPath string, conflictAt time.Time, actorID string) string {
	if actorID == "" {
		actorID = "unknown"
	}
	timestamp := conflictAt.UTC().Format("20060102T150405Z")
	filename := fmt.Sprintf("%s.conflict-%s-%s", filepath.Base(originalPath), timestamp, actorID)
	return filepath.Join(filepath.Dir(originalPath), filename)
}
