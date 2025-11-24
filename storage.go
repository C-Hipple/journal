package main

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
)

var (
	gitUsername string
	gitRepoName string
	githubToken string
	repoDir     = "journal_storage"
)

func initGitRepo() {
	log.Println("Initializing Git repo...")

	// Try to open the repo
	r, err := git.PlainOpen(repoDir)
	if err == git.ErrRepositoryNotExists {
		// Clone
		repoURL := fmt.Sprintf("https://github.com/%s/%s.git", gitUsername, gitRepoName)
		log.Printf("Cloning %s into %s...\n", repoURL, repoDir)

		_, err := git.PlainClone(repoDir, false, &git.CloneOptions{
			URL: repoURL,
			Auth: &githttp.BasicAuth{
				Username: gitUsername,
				Password: githubToken,
			},
			Progress: os.Stdout,
		})
		if err != nil {
			log.Printf("Error cloning repo: %v", err)
			return
		}
	} else if err != nil {
		log.Printf("Error opening repo: %v", err)
		return
	} else {
		// Pull
		log.Println("Pulling latest changes...")
		w, err := r.Worktree()
		if err != nil {
			log.Printf("Error getting worktree: %v", err)
		} else {
			err = w.Pull(&git.PullOptions{
				RemoteName: "origin",
				Auth: &githttp.BasicAuth{
					Username: gitUsername,
					Password: githubToken,
				},
				Progress: os.Stdout,
			})
			if err != nil && err != git.NoErrAlreadyUpToDate {
				log.Printf("Error pulling repo: %v", err)
			}
		}
	}

	// Check for journal.org
	journalPath := filepath.Join(repoDir, "journal.org")
	if _, err := os.Stat(journalPath); os.IsNotExist(err) {
		log.Println("Creating journal.org...")
		if err := os.WriteFile(journalPath, []byte(""), 0644); err != nil {
			log.Printf("Error creating journal.org: %v", err)
		}
	}
}

func syncGit() {
	log.Println("Syncing with Git...")

	r, err := git.PlainOpen(repoDir)
	if err != nil {
		log.Printf("Error opening repo: %v", err)
		return
	}

	w, err := r.Worktree()
	if err != nil {
		log.Printf("Error getting worktree: %v", err)
		return
	}

	// git add journal.org
	_, err = w.Add("journal.org")
	if err != nil {
		log.Printf("Error adding to git: %v", err)
		return
	}

	// git commit
	msg := fmt.Sprintf("Journal entry %s", time.Now().Format("2006-01-02 15:04"))
	_, err = w.Commit(msg, &git.CommitOptions{
		Author: &object.Signature{
			Name:  gitUsername,
			Email: gitUsername + "@users.noreply.github.com", // Fallback email
			When:  time.Now(),
		},
	})
	if err != nil {
		log.Printf("Error committing to git: %v", err)
		return
	}

	// git push
	err = r.Push(&git.PushOptions{
		Auth: &githttp.BasicAuth{
			Username: gitUsername,
			Password: githubToken,
		},
		Progress: os.Stdout,
	})
	if err != nil {
		log.Printf("Error pushing to git: %v", err)
		return
	}

	log.Println("Git sync successful.")
}

func formatOrgEntry(analysis JournalAnalysis) string {
	var sb bytes.Buffer

	sb.WriteString("** General Emotional Checkin\n")
	sb.WriteString(fmt.Sprintf("%s\n", analysis.EmotionalCheckin))

	sb.WriteString("** Things that made me happy\n")
	for _, item := range analysis.HappyThings {
		sb.WriteString(fmt.Sprintf("- %s\n", item))
	}

	sb.WriteString("** Things that were stressful\n")
	for _, item := range analysis.StressfulThings {
		sb.WriteString(fmt.Sprintf("- %s\n", item))
	}

	sb.WriteString("** Things I want to focus on doing for next time\n")
	for _, item := range analysis.FocusItems {
		sb.WriteString(fmt.Sprintf("- %s\n", item))
	}

	sb.WriteString("** Raw Input\n")
	sb.WriteString(fmt.Sprintf("%s\n", analysis.RawInput))

	sb.WriteString("\n")
	return sb.String()
}

func SaveEntry(entryContent string) {
	// Determine file path
	targetFile := "journal.org"
	if gitUsername != "" && gitRepoName != "" {
		targetFile = filepath.Join(repoDir, "journal.org")
	}

	// Read existing file
	existingBytes, err := os.ReadFile(targetFile)
	if err != nil && !os.IsNotExist(err) {
		log.Printf("Error reading %s: %v", targetFile, err)
		return
	}
	existingContent := string(existingBytes)

	dateHeader := time.Now().Format("* 2006-01-02 Mon")
	var newFileContent string

	if strings.Contains(existingContent, dateHeader) {
		// Append to existing entry
		// Find the location of the date header
		idx := strings.Index(existingContent, dateHeader)

		// Find the start of the NEXT top-level entry after this one
		// We look for "\n* " after the current header
		rest := existingContent[idx+len(dateHeader):]
		nextHeaderRelIdx := strings.Index(rest, "\n* ")

		if nextHeaderRelIdx == -1 {
			// No next entry, append to end
			newFileContent = existingContent + entryContent
		} else {
			// Insert before the next entry
			insertPos := idx + len(dateHeader) + nextHeaderRelIdx
			newFileContent = existingContent[:insertPos] + entryContent + existingContent[insertPos:]
		}
	} else {
		// Create new entry
		prefix := ""
		if len(existingContent) > 0 && !strings.HasSuffix(existingContent, "\n") {
			prefix = "\n"
		}
		newFileContent = existingContent + prefix + dateHeader + "\n" + entryContent
	}

	// Write back to file
	if err := os.WriteFile(targetFile, []byte(newFileContent), 0644); err != nil {
		log.Printf("Error writing to %s: %v", targetFile, err)
		return
	}

	// Sync with Git if enabled
	if gitUsername != "" && gitRepoName != "" && githubToken != "" {
		syncGit()
	}
}

func GetEntries() (string, error) {
	// Determine file path
	targetFile := "journal.org"
	if gitUsername != "" && gitRepoName != "" {
		targetFile = filepath.Join(repoDir, "journal.org")
	}

	// Read existing file
	existingBytes, err := os.ReadFile(targetFile)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return string(existingBytes), nil
}
