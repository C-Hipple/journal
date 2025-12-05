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
	journalFormat string // "org" or "markdown", default is "markdown"
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

	// Check for journal file
	journalFileName := getJournalFileName()
	journalPath := filepath.Join(repoDir, journalFileName)
	if _, err := os.Stat(journalPath); os.IsNotExist(err) {
		log.Printf("Creating %s...", journalFileName)
		if err := os.WriteFile(journalPath, []byte(""), 0644); err != nil {
			log.Printf("Error creating %s: %v", journalFileName, err)
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

	// git add journal file
	journalFileName := getJournalFileName()
	_, err = w.Add(journalFileName)
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

func getJournalFileName() string {
	if journalFormat == "org" {
		return "journal.org"
	}
	return "journal.md"
}

func getDateHeaderFormat() string {
	if journalFormat == "org" {
		return "* 2006-01-02 Mon"
	}
	return "## 2006-01-02 Mon"
}

func getTopLevelHeaderPattern() string {
	if journalFormat == "org" {
		return "\n* "
	}
	return "\n## "
}

func getSectionHeaderPattern() string {
	if journalFormat == "org" {
		return "\n** "
	}
	return "\n### "
}

func formatOrgEntry(analysis JournalAnalysis) string {
	var sb bytes.Buffer

	if journalFormat == "org" {
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
	} else {
		// Markdown format
		sb.WriteString("### General Emotional Checkin\n\n")
		sb.WriteString(fmt.Sprintf("%s\n\n", analysis.EmotionalCheckin))

		sb.WriteString("### Things that made me happy\n\n")
		for _, item := range analysis.HappyThings {
			sb.WriteString(fmt.Sprintf("- %s\n", item))
		}
		sb.WriteString("\n")

		sb.WriteString("### Things that were stressful\n\n")
		for _, item := range analysis.StressfulThings {
			sb.WriteString(fmt.Sprintf("- %s\n", item))
		}
		sb.WriteString("\n")

		sb.WriteString("### Things I want to focus on doing for next time\n\n")
		for _, item := range analysis.FocusItems {
			sb.WriteString(fmt.Sprintf("- %s\n", item))
		}
		sb.WriteString("\n")

		sb.WriteString("### Raw Input\n\n")
		sb.WriteString(fmt.Sprintf("%s\n\n", analysis.RawInput))
	}

	sb.WriteString("\n")
	return sb.String()
}

func SaveEntry(analysis JournalAnalysis) {
	// Determine file path
	journalFileName := getJournalFileName()
	targetFile := journalFileName
	if gitUsername != "" && gitRepoName != "" {
		targetFile = filepath.Join(repoDir, journalFileName)
	}

	// Read existing file
	existingBytes, err := os.ReadFile(targetFile)
	if err != nil && !os.IsNotExist(err) {
		log.Printf("Error reading %s: %v", targetFile, err)
		return
	}
	existingContent := string(existingBytes)

	dateHeader := time.Now().Format(getDateHeaderFormat())
	var newFileContent string

	if strings.Contains(existingContent, dateHeader) {
		// Merge into existing entry
		newFileContent = mergeEntry(existingContent, dateHeader, analysis)
	} else {
		// Create new entry
		entryContent := formatOrgEntry(analysis)
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

func mergeEntry(content string, dateHeader string, analysis JournalAnalysis) string {
	idx := strings.Index(content, dateHeader)
	before := content[:idx]

	rest := content[idx:]
	// Find end of this entry (start of next date)
	// We search in rest[len(dateHeader):] to avoid matching the current header if it was somehow ambiguous,
	// but mainly to find the *next* one.
	// We look for the top-level header pattern which signifies a new entry.
	topLevelPattern := getTopLevelHeaderPattern()
	nextEntryRelIdx := strings.Index(rest[len(dateHeader):], topLevelPattern)

	var entryBlock, after string
	if nextEntryRelIdx == -1 {
		entryBlock = rest
		after = ""
	} else {
		splitPos := len(dateHeader) + nextEntryRelIdx
		entryBlock = rest[:splitPos]
		after = rest[splitPos:]
	}

	// Modify entryBlock
	var emotionalHeader, happyHeader, stressfulHeader, focusHeader, rawHeader string
	if journalFormat == "org" {
		emotionalHeader = "** General Emotional Checkin"
		happyHeader = "** Things that made me happy"
		stressfulHeader = "** Things that were stressful"
		focusHeader = "** Things I want to focus on doing for next time"
		rawHeader = "** Raw Input"
	} else {
		emotionalHeader = "### General Emotional Checkin"
		happyHeader = "### Things that made me happy"
		stressfulHeader = "### Things that were stressful"
		focusHeader = "### Things I want to focus on doing for next time"
		rawHeader = "### Raw Input"
	}

	entryBlock = appendToSection(entryBlock, emotionalHeader, analysis.EmotionalCheckin)

	for _, item := range analysis.HappyThings {
		entryBlock = appendToSection(entryBlock, happyHeader, "- "+item)
	}

	for _, item := range analysis.StressfulThings {
		entryBlock = appendToSection(entryBlock, stressfulHeader, "- "+item)
	}

	for _, item := range analysis.FocusItems {
		entryBlock = appendToSection(entryBlock, focusHeader, "- "+item)
	}

	entryBlock = appendToSection(entryBlock, rawHeader, analysis.RawInput)

	return before + entryBlock + after
}

func appendToSection(entryBlock string, sectionHeader string, newItem string) string {
	idx := strings.Index(entryBlock, sectionHeader)
	if idx == -1 {
		// Section missing, append to end of block
		if !strings.HasSuffix(entryBlock, "\n") {
			entryBlock += "\n"
		}
		separator := "\n"
		if journalFormat == "markdown" {
			separator = "\n\n"
		}
		return entryBlock + sectionHeader + separator + newItem + "\n"
	}

	// Section exists
	// Find end of section (start of next section or end of block)
	// We look for the section header pattern after the header
	sectionPattern := getSectionHeaderPattern()
	rest := entryBlock[idx+len(sectionHeader):]
	nextSectionIdx := strings.Index(rest, sectionPattern)

	if nextSectionIdx == -1 {
		// No next section, append to end
		// Ensure there's a newline before appending if needed, though usually there is one.
		// We want to append at the very end.
		// If the block ends with newline, just append.
		suffix := ""
		if !strings.HasSuffix(entryBlock, "\n") {
			suffix = "\n"
		}
		return entryBlock + suffix + newItem + "\n"
	}

	// Insert before next section
	insertPos := idx + len(sectionHeader) + nextSectionIdx
	return entryBlock[:insertPos] + "\n" + newItem + entryBlock[insertPos:]
}

func GetEntries() (string, error) {
	// Determine file path
	journalFileName := getJournalFileName()
	targetFile := journalFileName
	if gitUsername != "" && gitRepoName != "" {
		targetFile = filepath.Join(repoDir, journalFileName)
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
