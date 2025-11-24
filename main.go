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

// Struct to parse the JSON response from Gemini
type JournalAnalysis struct {
    EmotionalCheckin string   `json:"emotional_checkin"`
    HappyThings      []string `json:"happy_things"`
    StressfulThings  []string `json:"stressful_things"`
    FocusItems       []string `json:"focus_items"`
	RawInput         string   `json:"-"` // Not from JSON, populated manually
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
        json.NewEncoder(w).Encode(map[string]string{"status": "logged_in"})
    })

    http.HandleFunc("/api/check-auth", func(w http.ResponseWriter, r *http.Request) {
        if !isAuthenticated(r) {
            http.Error(w, "Unauthorized", http.StatusUnauthorized)
            return
        }

        w.WriteHeader(http.StatusOK)
        json.NewEncoder(w).Encode(map[string]string{"status": "authenticated"})
    })

    http.HandleFunc("/api/entries", func(w http.ResponseWriter, r *http.Request) {
        if !isAuthenticated(r) {
            http.Error(w, "Unauthorized", http.StatusUnauthorized)
            return
        }

        if r.Method == http.MethodGet {
            entries, err := GetEntries()
            if err != nil {
                http.Error(w, "Failed to retrieve entries", http.StatusInternalServerError)
                return
            }
            w.WriteHeader(http.StatusOK)
            json.NewEncoder(w).Encode(map[string]string{"content": entries})
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
        go processEntry(req.Content)

        w.WriteHeader(http.StatusOK)
        json.NewEncoder(w).Encode(map[string]string{"status": "created"})
    })

    log.Println("Listening on :8080...")
    err := http.ListenAndServe(":8080", nil)
    if err != nil {
        log.Fatal(err)
    }
}



func processEntry(content string) {
    log.Printf("Processing entry: %s\n", content)

    if geminiToken == "" {
        log.Println("Skipping AI processing: GEMINI_API_TOKEN not set")
        return
    }

    jsonResponse, err := callGemini(content)
    if err != nil {
        log.Printf("Error calling Gemini: %v\n", err)
        return
    }

    log.Printf("Gemini Summary:\n%s\n", jsonResponse)

    // Parse the JSON response
    var analysis JournalAnalysis
    // Gemini might wrap the JSON in markdown code blocks (```json ... ```), so we might need to clean it.
    // For simplicity, let's assume it returns clean JSON or we strip the markdown if present.
    // A simple way to strip markdown code blocks:
    cleanJSON := stripMarkdown(jsonResponse)

    if err := json.Unmarshal([]byte(cleanJSON), &analysis); err != nil {
        log.Printf("Error unmarshaling Gemini response: %v\nRaw response: %s", err, jsonResponse)
        return
    }

	analysis.RawInput = content

    // Save the entry (merging if necessary)
    SaveEntry(analysis)
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



func callGemini(journalEntry string) (string, error) {
    url := "https://generativelanguage.googleapis.com/v1beta/models/gemini-2.5-flash:generateContent?key=" + geminiToken

    prompt := fmt.Sprintf(`Analyze the following journal entry and provide a structured response in JSON format.
The JSON should have the following fields:
- "emotional_checkin": A general assessment of the emotional state.
- "happy_things": A list of things that made the author happy.
- "stressful_things": A list of things that were stressful.
- "focus_items": A list of things the author wants to focus on for next time.

Journal Entry:
"%s"
`, journalEntry)

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
