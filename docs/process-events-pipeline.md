# Process Event Pipeline

This pipeline ingests newline-delimited JSON that describes local process or system events, filters out noise, infers high-value signals, and surfaces them through memory and the assistant feed.

## Input

- The path is configurable via `assistant_config.processEventsPath` (defaults to `~/.config/jot/assistant_process_events.jsonl`).
- Each line is parsed into `LocalProcessEvent`. Supported fields include `ts`, `timestamp`, `level`, `severity`, `process`, `service`, `message`, `detail`, `type`, `event`, `pid`, `cpuPercent`, `memoryMb`, `cmd`, `metadata`, and `tags`.
- Timestamps are normalized to UTC with fallbacks for `time.RFC3339`, `time.RFC3339Nano`, `time.RFC1123`, and `2006-01-02 15:04:05`.

## Transformations

1. **Normalize & deduplicate** – `localProcessEventFromRaw` normalizes names, commands, and metadata; `BuildProcessEventSignals` deduplicates events using a SHA-1 fingerprint over the process, type, level, message, and source ID.
2. **Noise filtering** – `filterProcessEvents` drops events older than 24 hours, no process name, known system helpers (e.g., `svchost`, `explorer`, `System Idle Process`), explicit heartbeat/poll types, and anything below `warn` severity that lacks crash-like keywords.
3. **Signal scoring** – `buildProcessEventMeta` assigns importance, confidence, and bucket based on severity, type, and duration (e.g., crash events get boosted +20, long-running events get +10).
4. **Memory ingestion** – Errors/warnings yield `MemoryObservation` entries scoped to `local-process`, and crashy/error spikes additionally create `MemoryInference` entries that describe instability.
5. **Feed creation** – High-importance or crashy events generate `AssistantFeedItem`s (`AssistantFeedKindFollowUpNeeded`) with links back to the command line, memory references, and confidence/importance scored the same as the memory artifacts.

## Operating model

- `daemonProactiveWorkHook` now loads events every loop, runs the pipeline, persists the resulting observations/inferences via `memory.AddObservation/AddInference`, and upserts feed items via `feed.AddItem`.
- Noise-resistant filtering plus deduplication prevents repeated low-value chatter, while severity heuristics keep the pipeline focused on actionable signals.

## Testing

- `assistant_event_pipeline_test.go` verifies crash events populate observations/inferences/feed items, that idle system events are ignored, and that loading JSON lines produces normalized events.
