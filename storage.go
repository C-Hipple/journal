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
	gitUsername   string
	gitRepoName   string
	githubToken   string
	repoDir       = "journal_storage"
	journalFormat string // "org" or "markdown", default is "markdown"
)

func getJournalFileName(base string) string {
	if journalFormat == "org" {
		return base + ".org"
	}
	return base + ".md"
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

func getMainHeaderPrefix() string {
	if journalFormat == "org" {
		return "* "
	}
	return "## "
}

func getSubHeaderPrefix() string {
	if journalFormat == "org" {
		return "** "
	}
	return "### "
}

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

	// Check for all journal files
	for _, config := range EntryTypes {
		fileName := getJournalFileName(config.TargetFile)
		journalPath := filepath.Join(repoDir, fileName)
		if _, err := os.Stat(journalPath); os.IsNotExist(err) {
			log.Printf("Creating %s...\n", fileName)
			if err := os.WriteFile(journalPath, []byte(""), 0644); err != nil {
				log.Printf("Error creating %s: %v", fileName, err)
			}
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

	// git add .
	_, err = w.Add(".")
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

func formatEntry(entryType string, analysis map[string]interface{}) string {
	var sb bytes.Buffer

	config, ok := EntryTypes[entryType]
	if !ok {
		config = EntryTypes["journal"]
	}

	headerPrefix := getSubHeaderPrefix()
	isMarkdown := journalFormat == "markdown"

	for _, field := range config.Fields {
		header, ok := HeaderMapping[field]
		if !ok {
			header = field
		}
		sb.WriteString(fmt.Sprintf("%s%s\n", headerPrefix, header))
		if isMarkdown {
			sb.WriteString("\n")
		}

		if val, ok := analysis[field]; ok {
			switch v := val.(type) {
			case string:
				sb.WriteString(v + "\n")
			case []interface{}:
				for _, item := range v {
					sb.WriteString(fmt.Sprintf("- %v\n", item))
				}
			case []string:
				for _, item := range v {
					sb.WriteString(fmt.Sprintf("- %s\n", item))
				}
			}
		}
		if isMarkdown {
			sb.WriteString("\n")
		}
	}

	if raw, ok := analysis["RawInput"].(string); ok {
		sb.WriteString(fmt.Sprintf("%sRaw Input\n", headerPrefix))
		if isMarkdown {
			sb.WriteString("\n")
		}
		sb.WriteString(raw + "\n")
		if isMarkdown {
			sb.WriteString("\n")
		}
	}

	sb.WriteString("\n")
	return sb.String()
}

func SaveEntry(entryType string, analysis map[string]interface{}) {
	config, ok := EntryTypes[entryType]
	if !ok {
		config = EntryTypes["journal"]
	}

	fileName := getJournalFileName(config.TargetFile)
	targetFile := fileName
	if gitUsername != "" && gitRepoName != "" {
		targetFile = filepath.Join(repoDir, fileName)
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
		newFileContent = mergeEntry(entryType, existingContent, dateHeader, analysis)
	} else {
		// Create new entry
		entryContent := formatEntry(entryType, analysis)
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

func mergeEntry(entryType string, content string, dateHeader string, analysis map[string]interface{}) string {
	idx := strings.Index(content, dateHeader)
	before := content[:idx]

	rest := content[idx:]
	// Find end of this entry (start of next date)
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

	config, ok := EntryTypes[entryType]
	if !ok {
		config = EntryTypes["journal"]
	}

	headerPrefix := getSubHeaderPrefix()

	for _, field := range config.Fields {
		header, ok := HeaderMapping[field]
		if !ok {
			header = field
		}
		sectionHeader := headerPrefix + header

		if val, ok := analysis[field]; ok {
			switch v := val.(type) {
			case string:
				entryBlock = appendToSection(entryBlock, sectionHeader, v)
			case []interface{}:
				for _, item := range v {
					entryBlock = appendToSection(entryBlock, sectionHeader, "- "+fmt.Sprint(item))
				}
			case []string:
				for _, item := range v {
					entryBlock = appendToSection(entryBlock, sectionHeader, "- "+item)
				}
			}
		}
	}

	if raw, ok := analysis["RawInput"].(string); ok {
		entryBlock = appendToSection(entryBlock, headerPrefix+"Raw Input", raw)
	}

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
	sectionPattern := getSectionHeaderPattern()
	rest := entryBlock[idx+len(sectionHeader):]
	nextSectionIdx := strings.Index(rest, sectionPattern)

	if nextSectionIdx == -1 {
		// No next section, append to end
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

func GetEntries(entryType string) (string, error) {
	config, ok := EntryTypes[entryType]
	if !ok {
		config = EntryTypes["journal"]
	}

	fileName := getJournalFileName(config.TargetFile)
	targetFile := fileName
	if gitUsername != "" && gitRepoName != "" {
		targetFile = filepath.Join(repoDir, fileName)
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
