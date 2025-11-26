// Package usage provides usage tracking and logging functionality for the CLI Proxy API server.
package usage

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// UsageEvent represents a single API request event for persistence.
// This struct captures the essential metrics for each request.
type UsageEvent struct {
	Timestamp        time.Time `json:"timestamp"`
	Model            string    `json:"model"`
	PromptTokens     int64     `json:"prompt_tokens"`
	CompletionTokens int64     `json:"completion_tokens"`
	TotalTokens      int64     `json:"total_tokens"`
	Status           int       `json:"status"`
	RequestID        string    `json:"request_id,omitempty"`
	APIKeyHash       string    `json:"api_key_hash,omitempty"`
}

// JSONStore provides append-only JSON Lines storage for usage events.
// Each event is written as a single line of JSON, making it easy to parse
// and append without loading the entire file into memory.
type JSONStore struct {
	path   string
	mu     sync.Mutex
	buffer []UsageEvent
	file   *os.File
	ticker *time.Ticker
	done   chan struct{}
}

// NewJSONStore creates a new JSON store at the specified path.
// The file will be created if it doesn't exist, or opened for append if it does.
// A background goroutine will periodically flush buffered events every 30 seconds.
//
// Parameters:
//   - path: The file path where usage events will be stored
//
// Returns:
//   - *JSONStore: A new JSON store instance
func NewJSONStore(path string) *JSONStore {
	s := &JSONStore{
		path:   path,
		buffer: make([]UsageEvent, 0, 50),
		ticker: time.NewTicker(30 * time.Second),
		done:   make(chan struct{}),
	}

	// Start periodic flush goroutine
	go s.periodicFlush()

	return s
}

// Write adds a usage event to the store's buffer.
// Events are buffered in memory and periodically flushed to disk for performance.
// This method is thread-safe and non-blocking.
//
// Parameters:
//   - event: The usage event to persist
//
// Returns:
//   - error: An error if the write operation fails
func (s *JSONStore) Write(event UsageEvent) error {
	if s == nil {
		return fmt.Errorf("json store is nil")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.buffer = append(s.buffer, event)

	// Auto-flush if buffer gets large (50 events)
	if len(s.buffer) >= 50 {
		return s.flushLocked()
	}

	return nil
}

// Flush writes all buffered events to disk.
// This should be called periodically and before shutdown to ensure data persistence.
//
// Returns:
//   - error: An error if the flush operation fails
func (s *JSONStore) Flush() error {
	if s == nil {
		return fmt.Errorf("json store is nil")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	return s.flushLocked()
}

// flushLocked performs the actual flush operation.
// Must be called with s.mu held.
func (s *JSONStore) flushLocked() error {
	if len(s.buffer) == 0 {
		return nil
	}

	// Ensure directory exists
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Open file for append (create if doesn't exist)
	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer f.Close()

	// Write each event as a JSON line
	encoder := json.NewEncoder(f)
	for i := range s.buffer {
		if err := encoder.Encode(&s.buffer[i]); err != nil {
			return fmt.Errorf("failed to encode event: %w", err)
		}
	}

	// Sync to disk
	if err := f.Sync(); err != nil {
		return fmt.Errorf("failed to sync file: %w", err)
	}

	// Clear buffer after successful write
	s.buffer = s.buffer[:0]

	return nil
}

// periodicFlush runs in a background goroutine and flushes buffered events every 30 seconds.
// This ensures that events are persisted even if the buffer doesn't fill up.
func (s *JSONStore) periodicFlush() {
	for {
		select {
		case <-s.ticker.C:
			// Periodic flush every 30 seconds
			if err := s.Flush(); err != nil {
				fmt.Fprintf(os.Stderr, "periodic flush error: %v\n", err)
			}
		case <-s.done:
			// Stop signal received
			return
		}
	}
}

// Load reads all usage events from the file.
// This is typically called on server startup to restore historical data.
//
// Returns:
//   - []UsageEvent: All events stored in the file
//   - error: An error if the load operation fails
func (s *JSONStore) Load() ([]UsageEvent, error) {
	if s == nil {
		return nil, fmt.Errorf("json store is nil")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if file exists
	if _, err := os.Stat(s.path); os.IsNotExist(err) {
		// File doesn't exist yet, return empty slice
		return []UsageEvent{}, nil
	}

	// Open file for reading
	f, err := os.Open(s.path)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer f.Close()

	// Read events line by line
	var events []UsageEvent
	scanner := bufio.NewScanner(f)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := scanner.Bytes()

		// Skip empty lines
		if len(line) == 0 {
			continue
		}

		var event UsageEvent
		if err := json.Unmarshal(line, &event); err != nil {
			// Log warning but continue reading other events
			fmt.Fprintf(os.Stderr, "warning: failed to parse event on line %d: %v\n", lineNum, err)
			continue
		}

		events = append(events, event)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	return events, nil
}

// Close flushes any remaining buffered events and closes the store.
// This should be called before application shutdown.
//
// Returns:
//   - error: An error if the close operation fails
func (s *JSONStore) Close() error {
	if s == nil {
		return nil
	}

	// Stop the periodic flush goroutine
	if s.ticker != nil {
		s.ticker.Stop()
	}
	if s.done != nil {
		close(s.done)
	}

	// Flush any remaining events
	if err := s.Flush(); err != nil {
		return err
	}

	return nil
}

// Len returns the number of events currently in the buffer (not yet flushed).
func (s *JSONStore) Len() int {
	if s == nil {
		return 0
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	return len(s.buffer)
}

