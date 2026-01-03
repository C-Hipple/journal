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

func isAuthenticated(r *http.Request) bool {
    cookie, err := r.Cookie("journal_session")
    if err != nil {
        return false
    }

    sessionMutex.RLock()
    defer sessionMutex.RUnlock()
    return cookie.Value == validSessionHash && validSessionHash != ""
}

func main() {
    // Get password from env
    expectedPassword := os.Getenv("JOURNAL_PASSWORD")
    if expectedPassword == "" {
        log.Fatal("Error: JOURNAL_PASSWORD environment variable not set.")
    }
    log.Printf("JOURNAL_PASSWORD: %s", expectedPassword)

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

        w.WriteHeader(http.StatusOK)
        if err := json.NewEncoder(w).Encode(map[string]string{"status": "logged_in"}); err != nil {
            log.Printf("Error encoding response: %v", err)
        }
    })

    http.HandleFunc("/api/check-auth", func(w http.ResponseWriter, r *http.Request) {
        if !isAuthenticated(r) {
            http.Error(w, "Unauthorized", http.StatusUnauthorized)
            return
        }

        w.WriteHeader(http.StatusOK)
        if err := json.NewEncoder(w).Encode(map[string]string{"status": "authenticated"}); err != nil {
            log.Printf("Error encoding response: %v", err)
        }
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

        w.WriteHeader(http.StatusOK)
        if err := json.NewEncoder(w).Encode(types); err != nil {
            log.Printf("Error encoding response: %v", err)
        }
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
            w.WriteHeader(http.StatusOK)
            if err := json.NewEncoder(w).Encode(map[string]string{"content": entries}); err != nil {
                log.Printf("Error encoding response: %v", err)
            }
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

        // Process the entry asynchronously
        if req.Type == "" {
            req.Type = "journal"
        }
        go processEntry(req.Content, req.Type)

        w.WriteHeader(http.StatusOK)
        if err := json.NewEncoder(w).Encode(map[string]string{"status": "created"}); err != nil {
            log.Printf("Error encoding response: %v", err)
        }
    })

    log.Println("Listening on :8080...")
    err := http.ListenAndServe(":8080", nil)
    if err != nil {
        log.Fatal(err)
    }
}



func processEntry(content string, entryType string) {
	log.Printf("Processing %s entry: %s\n", entryType, content)

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

	prompt := fmt.Sprintf(config.Prompt, content)
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

	analysis["RawInput"] = content

	// Save the entry (merging if necessary)
	SaveEntry(entryType, analysis)
}



func stripMarkdown(s string) string {
    // Remove ```json at start and ``` at end if present
    // This is a basic implementation
    if len(s) > 7 && s[:7] == "```json" {
        s = s[7:]
    }
    if len(s) > 3 && s[:3] == "```" {
        s = s[3:]
    }
    if len(s) > 3 && s[len(s)-3:] == "```" {
        s = s[:len(s)-3]
    }
    return s
}



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
// ...

    jsonData, err := json.Marshal(reqBody)
    if err != nil {
        return "", err
    }

    resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonData))
    if err != nil {
        return "", err
    }
    defer resp.Body.Close()

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
