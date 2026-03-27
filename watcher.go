package main

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type EventType string

const (
	EventCreate EventType = "create"
	EventModify EventType = "modify"
	EventDelete EventType = "delete"
)

type FileEvent struct {
	Path string
	Type EventType
}

type fileState struct {
	modTime time.Time
	size    int64
}

type Watcher struct {
	dir   string
	state map[string]fileState
}

func NewWatcher(dir string) (*Watcher, error) {
	state, err := snapshotDir(dir)
	if err != nil {
		return nil, err
	}

	return &Watcher{dir: dir, state: state}, nil
}

func (w *Watcher) Poll() ([]FileEvent, error) {
	current, err := snapshotDir(w.dir)
	if err != nil {
		return nil, err
	}

	var events []FileEvent
	for path, cur := range current {
		prev, ok := w.state[path]
		if !ok {
			events = append(events, FileEvent{Path: path, Type: EventCreate})
			continue
		}
		if cur.modTime != prev.modTime || cur.size != prev.size {
			events = append(events, FileEvent{Path: path, Type: EventModify})
		}
	}

	for path := range w.state {
		if _, ok := current[path]; !ok {
			events = append(events, FileEvent{Path: path, Type: EventDelete})
		}
	}

	sort.Slice(events, func(i, j int) bool {
		if events[i].Path == events[j].Path {
			return events[i].Type < events[j].Type
		}
		return events[i].Path < events[j].Path
	})

	w.state = current
	return events, nil
}

func (w *Watcher) Start(ctx context.Context, interval time.Duration) (<-chan FileEvent, <-chan error) {
	events := make(chan FileEvent)
	errs := make(chan error, 1)

	go func() {
		defer close(events)
		defer close(errs)

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				batch, err := w.Poll()
				if err != nil {
					errs <- err
					return
				}
				for _, event := range batch {
					select {
					case <-ctx.Done():
						return
					case events <- event:
					}
				}
			}
		}
	}()

	return events, errs
}

func snapshotDir(dir string) (map[string]fileState, error) {
	state := make(map[string]fileState)
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		state[path] = fileState{modTime: info.ModTime(), size: info.Size()}
		return nil
	})
	if err != nil {
		if os.IsNotExist(err) {
			return state, nil
		}
		return nil, err
	}
	return state, nil
}
