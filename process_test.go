package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeGemini points callGemini at a server that answers each prompt with the
// JSON respond returns for it, and returns a function listing the prompts sent
// so far.
func fakeGemini(t *testing.T, respond func(prompt string) string) func() []string {
	t.Helper()
	var (
		mu      sync.Mutex
		prompts []string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req GeminiRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Contents) == 0 || len(req.Contents[0].Parts) == 0 {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		prompt := req.Contents[0].Parts[0].Text
		mu.Lock()
		prompts = append(prompts, prompt)
		mu.Unlock()

		// Models tend to fence their JSON, which processEntry strips.
		text, err := json.Marshal("```json\n" + respond(prompt) + "\n```")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"candidates": [{"content": {"parts": [{"text": %s}]}}]}`, text)
	}))
	t.Cleanup(server.Close)

	origEndpoint, origToken := geminiEndpoint, geminiToken
	geminiEndpoint, geminiToken = server.URL, "test-token"
	t.Cleanup(func() { geminiEndpoint, geminiToken = origEndpoint, origToken })

	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), prompts...)
	}
}

// Every store runs through the same processing: a note on its own is analysed
// on its own, and a topic is re-synthesised from all of its notes each time one
// is added.
func TestProcessEntry(t *testing.T) {
	stores := map[string]func(t *testing.T) Store{
		"file":     func(t *testing.T) Store { return fileStore{} },
		"database": func(t *testing.T) Store { return newTestSQLStore(t) },
	}
	for name, open := range stores {
		t.Run(name, func(t *testing.T) {
			setupStorageTest(t, "markdown")
			store := open(t)
			ctx := context.Background()
			prompts := fakeGemini(t, func(prompt string) string {
				if strings.Contains(prompt, "Journal Entry:") {
					return `{"emotional_checkin": "content", "happy_things": ["a long walk"]}`
				}
				notes := strings.Count(prompt, "note about")
				return fmt.Sprintf(`{"summary": "synthesis of %d notes", "notes": ["point %d"]}`, notes, notes)
			})
			now := time.Now()

			journal := EntryKey{Type: "journal", Day: now}
			id, err := store.SaveRawEntry(ctx, journal, "went for a long walk")
			if err != nil {
				t.Fatalf("SaveRawEntry failed: %v", err)
			}
			processEntry(store, journal, id, "went for a long walk")

			content, err := store.GetEntries(ctx, "journal")
			if err != nil {
				t.Fatalf("GetEntries failed: %v", err)
			}
			for _, want := range []string{"### General Emotional Checkin\n\ncontent\n", "- a long walk", "went for a long walk"} {
				if !strings.Contains(content, want) {
					t.Errorf("missing %q in:\n%s", want, content)
				}
			}

			talk := EntryKey{Type: "notes", Day: now, Topic: "Keynote"}
			for _, note := range []string{"note about the roadmap", "note about hiring"} {
				id, err := store.SaveRawEntry(ctx, talk, note)
				if err != nil {
					t.Fatalf("SaveRawEntry failed: %v", err)
				}
				processEntry(store, talk, id, note)
			}

			sent := prompts()
			if len(sent) != 3 {
				t.Fatalf("expected 3 prompts, got %d", len(sent))
			}
			if last := sent[2]; !strings.Contains(last, "note about the roadmap") || !strings.Contains(last, "note about hiring") {
				t.Errorf("the topic's synthesis prompt should cover every note, got:\n%s", last)
			}

			content, err = store.GetEntries(ctx, "notes")
			if err != nil {
				t.Fatalf("GetEntries failed: %v", err)
			}
			if !strings.Contains(content, "synthesis of 2 notes") || strings.Contains(content, "synthesis of 1 notes") {
				t.Errorf("expected only the latest synthesis:\n%s", content)
			}
			if strings.Count(content, "#### Summary") != 1 {
				t.Errorf("expected one summary for the topic:\n%s", content)
			}
		})
	}
}
