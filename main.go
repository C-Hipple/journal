package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

var (
	// Store the valid session hash in memory.
	validSessionHash string
	sessionMutex     sync.RWMutex
	geminiToken      string
)

type LoginRequest struct {
	Password string `json:"password"`
}

type EntryRequest struct {
	Content string `json:"content"`
	Type    string `json:"type"`
	Topic   string `json:"topic"`
}

// Gemini API Structs
type GeminiRequest struct {
	Contents []GeminiContent `json:"contents"`
}

type GeminiContent struct {
	Parts []GeminiPart `json:"parts"`
}

type GeminiPart struct {
	Text string `json:"text"`
}

type GeminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
}

// HeaderMapping defines the display headers for Org mode
var HeaderMapping = map[string]string{
	"emotional_checkin": "General Emotional Checkin",
	"happy_things":      "Things that made me happy",
	"stressful_things":  "Things that were stressful",
	"focus_items":       "Things I want to focus on doing for next time",
	"summary":           "Summary",
	"notes":             "Notes",
}

type EntryTypeConfig struct {
	Name       string
	Prompt     string
	TargetFile string
	Fields     []string
}

var EntryTypes = map[string]EntryTypeConfig{
	"journal": {
		Name: "Journal",
		Prompt: `Analyze the following journal entry and provide a structured response in JSON format.
The JSON should have the following fields:
- "emotional_checkin": A general assessment of the emotional state.
- "happy_things": A list of things that made the author happy.
- "stressful_things": A list of things that were stressful.
- "focus_items": A list of things the author wants to focus on for next time.

Journal Entry:
"%s"
`,
		TargetFile: "journal",
		Fields:     []string{"emotional_checkin", "happy_things", "stressful_things", "focus_items"},
	},
	"notes": {
		Name: "Notes",
		Prompt: `Structure the following thought into bullet notes and include a key summary. Provide a structured response in JSON format.
The JSON should have the following fields:
- "summary": A brief summary of the thought.
- "notes": A list of bullet points.

Thought:
"%s"
`,
		TargetFile: "notes",
		Fields:     []string{"summary", "notes"},
	},
}

// topicSynthesisPreamble frames a topic's accumulated notes so the model
// summarises the session as a whole instead of treating each note as its own
// entry.
const topicSynthesisPreamble = `The text below is the full set of running notes taken during "%s". They were jotted down over the course of a single session, so treat them as one continuous set of notes on that one topic: synthesize them as a whole, cover all of them, and do not treat each line as a separate entry.

`

var (
	synthesisLocksMutex sync.Mutex
	synthesisLocks      = map[string]*sync.Mutex{}
)

// synthesisLock serializes the read-notes, summarize, write-back cycle for a
// single topic so two notes posted in quick succession can't overwrite each
// other's synthesis.
func synthesisLock(key string) *sync.Mutex {
	synthesisLocksMutex.Lock()
	defer synthesisLocksMutex.Unlock()

	lock, ok := synthesisLocks[key]
	if !ok {
		lock = &sync.Mutex{}
		synthesisLocks[key] = lock
	}
	return lock
}

func isAuthenticated(r *http.Request) bool {
	cookie, err := r.Cookie("journal_session")
	if err != nil {
		return false
	}

	sessionMutex.RLock()
	defer sessionMutex.RUnlock()
	return cookie.Value == validSessionHash && validSessionHash != ""
}

// resolveEntryType falls back to the journal type for anything unrecognised.
func resolveEntryType(entryType string) string {
	if _, ok := EntryTypes[entryType]; ok {
		return entryType
	}
	if entryType != "" {
		log.Printf("Unknown entry type: %s, falling back to journal\n", entryType)
	}
	return "journal"
}

func writeJSON(w http.ResponseWriter, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("Error encoding response: %v", err)
	}
}

func main() {
	// Get password from env
	expectedPassword := os.Getenv("JOURNAL_PASSWORD")
	if expectedPassword == "" {
		log.Fatal("Error: JOURNAL_PASSWORD environment variable not set.")
	}

	// Get Gemini Token
	geminiToken = os.Getenv("GEMINI_API_TOKEN")
	if geminiToken == "" {
		log.Println("Warning: GEMINI_API_TOKEN environment variable not set. AI summarization will fail.")
	}

	// Get Journal Format (default to markdown)
	journalFormat = os.Getenv("JOURNAL_FORMAT")
	if journalFormat == "" {
		journalFormat = "markdown"
	}
	if journalFormat != "org" && journalFormat != "markdown" {
		log.Printf("Warning: JOURNAL_FORMAT must be 'org' or 'markdown', defaulting to 'markdown'")
		journalFormat = "markdown"
	}
	log.Printf("JOURNAL_FORMAT: %s", journalFormat)

	// Get Git Config
	gitUsername = os.Getenv("GIT_USERNAME")
	gitRepoName = os.Getenv("GIT_REPO_NAME")
	githubToken = os.Getenv("GITHUB_TOKEN")
	log.Printf("GIT_USERNAME: %s", gitUsername)
	log.Printf("GIT_REPO_NAME: %s", gitRepoName)
	if gitUsername != "" && gitRepoName != "" && githubToken != "" {
		initGitRepo()
	} else {
		log.Println("Warning: GIT_USERNAME, GIT_REPO_NAME, or GITHUB_TOKEN not set. Git storage disabled.")
	}

	// Serve static files
	fs := http.FileServer(http.Dir("./frontend/build"))
	http.Handle("/", fs)

	// API Endpoints
	http.HandleFunc("/api/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req LoginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		if req.Password != expectedPassword {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Generate a session hash
		hash := sha256.Sum256([]byte(req.Password + time.Now().String()))
		sessionToken := hex.EncodeToString(hash[:])

		// Store it in memory
		sessionMutex.Lock()
		validSessionHash = sessionToken
		sessionMutex.Unlock()

		// Set cookie
		http.SetCookie(w, &http.Cookie{
			Name:     "journal_session",
			Value:    sessionToken,
			Path:     "/",
			HttpOnly: true,
			Expires:  time.Now().Add(24 * time.Hour),
		})

		writeJSON(w, map[string]string{"status": "logged_in"})
	})

	http.HandleFunc("/api/check-auth", func(w http.ResponseWriter, r *http.Request) {
		if !isAuthenticated(r) {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		writeJSON(w, map[string]string{"status": "authenticated"})
	})

	http.HandleFunc("/api/types", func(w http.ResponseWriter, r *http.Request) {
		if !isAuthenticated(r) {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		type TypeInfo struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		var types []TypeInfo
		for id, config := range EntryTypes {
			types = append(types, TypeInfo{ID: id, Name: config.Name})
		}

		writeJSON(w, types)
	})

	// Topics already used today, so the UI can offer them instead of making
	// the author retype a talk title for every note.
	http.HandleFunc("/api/topics", func(w http.ResponseWriter, r *http.Request) {
		if !isAuthenticated(r) {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		entryType := resolveEntryType(r.URL.Query().Get("type"))
		topics, err := ListTopics(entryType, time.Now().Format(getDateHeaderFormat()))
		if err != nil {
			log.Printf("Error listing topics: %v", err)
			http.Error(w, "Failed to list topics", http.StatusInternalServerError)
			return
		}
		if topics == nil {
			topics = []string{}
		}

		writeJSON(w, map[string]interface{}{"topics": topics})
	})

	// Photo attachments. The frontend posts a (downscaled) camera capture here;
	// it is written into the storage repo and linked from the entry.
	http.HandleFunc("/api/photos", func(w http.ResponseWriter, r *http.Request) {
		if !isAuthenticated(r) {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, maxPhotoBytes)
		if err := r.ParseMultipartForm(maxPhotoBytes); err != nil {
			http.Error(w, "Photo too large or malformed", http.StatusBadRequest)
			return
		}

		file, _, err := r.FormFile("photo")
		if err != nil {
			http.Error(w, "Missing photo", http.StatusBadRequest)
			return
		}
		defer func() { _ = file.Close() }()

		data, err := io.ReadAll(file)
		if err != nil {
			http.Error(w, "Failed to read photo", http.StatusBadRequest)
			return
		}

		entryType := resolveEntryType(r.FormValue("type"))
		topic := sanitizeTopic(r.FormValue("topic"))

		now := time.Now()
		relPath, err := SavePhoto(now, topic, data)
		if err != nil {
			log.Printf("Error saving photo: %v", err)
			http.Error(w, "Failed to save photo", http.StatusBadRequest)
			return
		}

		caption := now.Format("15:04")
		if topic != "" {
			caption = topic + " " + caption
		}
		reference := photoReference(relPath, caption)
		if err := SavePhotoReference(entryType, now.Format(getDateHeaderFormat()), topic, reference); err != nil {
			http.Error(w, "Failed to record photo", http.StatusInternalServerError)
			return
		}

		writeJSON(w, map[string]string{
			"status": "created",
			"path":   relPath,
			"url":    "/api/media/" + relPath,
		})
	})

	// Serve stored photos back to the UI.
	http.HandleFunc("/api/media/", func(w http.ResponseWriter, r *http.Request) {
		if !isAuthenticated(r) {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		filePath, ok := mediaFilePath(r.URL.Path)
		if !ok {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, filePath)
	})

	http.HandleFunc("/api/entries", func(w http.ResponseWriter, r *http.Request) {
		if !isAuthenticated(r) {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		if r.Method == http.MethodGet {
			entryType := r.URL.Query().Get("type")
			if entryType == "" {
				entryType = "journal"
			}
			entries, err := GetEntries(entryType)
			if err != nil {
				http.Error(w, "Failed to retrieve entries", http.StatusInternalServerError)
				return
			}
			writeJSON(w, map[string]string{"content": entries})
			return
		}

		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req EntryRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		if strings.TrimSpace(req.Content) == "" {
			http.Error(w, "Content cannot be empty", http.StatusBadRequest)
			return
		}

		req.Type = resolveEntryType(req.Type)
		topic := sanitizeTopic(req.Topic)

		// Persist the raw input synchronously so it cannot be lost if AI
		// processing fails, and so it is visible in the log immediately.
		dateHeader := time.Now().Format(getDateHeaderFormat())
		if err := SaveRawEntry(req.Type, req.Content, dateHeader, topic); err != nil {
			http.Error(w, "Failed to save entry", http.StatusInternalServerError)
			return
		}

		// Enrich the saved entry with AI analysis asynchronously
		go processEntry(req.Content, req.Type, dateHeader, topic)

		writeJSON(w, map[string]string{"status": "created"})
	})

	log.Println("Listening on :8080...")
	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		log.Fatal(err)
	}
}

// processEntry runs the AI pass over a newly saved entry. Without a topic that
// means summarising the single note; with one it means re-synthesising every
// note taken under that topic so far, replacing the topic's previous synthesis.
func processEntry(content string, entryType string, dateHeader string, topic string) {
	log.Printf("Processing %s entry (topic %q): %s\n", entryType, topic, content)

	config, ok := EntryTypes[entryType]
	if !ok {
		log.Printf("Unknown entry type: %s, falling back to journal\n", entryType)
		config = EntryTypes["journal"]
		entryType = "journal"
	}

	if geminiToken == "" {
		log.Println("Skipping AI processing: GEMINI_API_TOKEN not set")
		return
	}

	if topic != "" {
		lock := synthesisLock(entryType + "\x00" + dateHeader + "\x00" + topic)
		lock.Lock()
		defer lock.Unlock()
	}

	// A topic is summarised as a whole, from every note taken under it.
	prompt := fmt.Sprintf(config.Prompt, content)
	if topic != "" {
		notes, err := GetTopicNotes(entryType, dateHeader, topic)
		if err != nil {
			log.Printf("Error reading notes for topic %q: %v", topic, err)
		} else if strings.TrimSpace(notes) != "" {
			prompt = fmt.Sprintf(config.Prompt, notes)
		}
		prompt = fmt.Sprintf(topicSynthesisPreamble, topic) + prompt
	}

	jsonResponse, err := callGemini(prompt)
	if err != nil {
		log.Printf("Error calling Gemini: %v\n", err)
		return
	}

	log.Printf("Gemini Summary:\n%s\n", jsonResponse)

	// Parse the JSON response
	var analysis map[string]interface{}
	cleanJSON := stripMarkdown(jsonResponse)

	if err := json.Unmarshal([]byte(cleanJSON), &analysis); err != nil {
		log.Printf("Error unmarshaling Gemini response: %v\nRaw response: %s", err, jsonResponse)
		return
	}

	// The raw input and any photos were already saved; the model only supplies
	// the analysis sections.
	delete(analysis, "RawInput")
	for _, section := range trailingSections {
		delete(analysis, section)
	}

	if topic != "" {
		if err := ReplaceTopicAnalysis(entryType, analysis, dateHeader, topic); err != nil {
			log.Printf("Error saving synthesis for topic %q: %v", topic, err)
		}
		return
	}

	if err := SaveEntry(entryType, analysis, dateHeader, ""); err != nil {
		log.Printf("Error saving analysis for %s entry: %v", entryType, err)
	}
}

func stripMarkdown(s string) string {
	// Remove a ```json / ``` fence around the response if present
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}

// geminiClient bounds the API call: a topic's synthesis lock is held across it,
// so a request that never returns would wedge every later note on that topic.
var geminiClient = &http.Client{Timeout: 60 * time.Second}

func callGemini(prompt string) (string, error) {
	url := "https://generativelanguage.googleapis.com/v1beta/models/gemini-2.5-flash:generateContent?key=" + geminiToken

	reqBody := GeminiRequest{
		Contents: []GeminiContent{
			{
				Parts: []GeminiPart{
					{Text: prompt},
				},
			},
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	resp, err := geminiClient.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var geminiResp GeminiResponse
	if err := json.NewDecoder(resp.Body).Decode(&geminiResp); err != nil {
		return "", err
	}

	if len(geminiResp.Candidates) > 0 && len(geminiResp.Candidates[0].Content.Parts) > 0 {
		return geminiResp.Candidates[0].Content.Parts[0].Text, nil
	}

	return "", fmt.Errorf("no content in response")
}
