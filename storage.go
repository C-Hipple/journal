package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
	imagesDir     = "images"
	journalFormat string // "org" or "markdown", default is "markdown"

	// storageMutex serializes all read-modify-write cycles on the journal
	// files (and the git operations that follow them) so concurrent entries
	// can't clobber each other.
	storageMutex sync.RWMutex
)

// topicHeaderLabel prefixes a topic's header text so a topic grouping is
// distinguishable from an ordinary section at the same heading depth.
const topicHeaderLabel = "Topic: "

// maxTopicLength caps a topic so it stays a readable single header line.
const maxTopicLength = 120

// trailingSections are kept at the bottom of an entry, in this order, so that
// analysis merged in later always lands above them.
var trailingSections = []string{"Photos", "Raw Input"}

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

func getSubHeaderPrefix() string {
	if journalFormat == "org" {
		return "** "
	}
	return "### "
}

// getTopicHeaderPrefix is the heading depth a topic grouping lives at: directly
// below the day's header, alongside ungrouped sections.
func getTopicHeaderPrefix() string {
	return getSubHeaderPrefix()
}

// getTopicSubHeaderPrefix is the heading depth of sections nested inside a
// topic grouping.
func getTopicSubHeaderPrefix() string {
	if journalFormat == "org" {
		return "*** "
	}
	return "#### "
}

func getTopicSectionPattern() string {
	if journalFormat == "org" {
		return "\n*** "
	}
	return "\n#### "
}

// topicHeaderLine renders the full header line for a topic.
func topicHeaderLine(topic string) string {
	return getTopicHeaderPrefix() + topicHeaderLabel + topic
}

// sectionLevel describes the heading depth that sections are written at: either
// directly under the day's header, or nested inside a topic grouping.
type sectionLevel struct {
	prefix  string // header prefix, e.g. "### "
	pattern string // start of the next section at this depth, e.g. "\n### "
}

func dateSectionLevel() sectionLevel {
	return sectionLevel{prefix: getSubHeaderPrefix(), pattern: getSectionHeaderPattern()}
}

func topicSectionLevel() sectionLevel {
	return sectionLevel{prefix: getTopicSubHeaderPrefix(), pattern: getTopicSectionPattern()}
}

// sanitizeTopic keeps a topic usable as a single header line.
func sanitizeTopic(topic string) string {
	topic = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		return r
	}, topic)
	topic = strings.TrimSpace(topic)
	if runes := []rune(topic); len(runes) > maxTopicLength {
		topic = strings.TrimSpace(string(runes[:maxTopicLength]))
	}
	return topic
}

// storageRepoURL is where the storage repo is cloned from. It's a variable so
// tests can point it at a local repository.
var storageRepoURL = func() string {
	return fmt.Sprintf("https://github.com/%s/%s.git", gitUsername, gitRepoName)
}

// initGitRepo clones the storage repo, or pulls it when it is already on disk.
// It returns an error when the repo can't be put on disk; a failed pull is only
// logged, since the local copy is still usable.
func initGitRepo() error {
	log.Println("Initializing Git repo...")

	// Try to open the repo
	r, err := git.PlainOpen(repoDir)
	if err == git.ErrRepositoryNotExists {
		// Clone
		repoURL := storageRepoURL()
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
			return fmt.Errorf("cloning %s: %w", repoURL, err)
		}
	} else if err != nil {
		return fmt.Errorf("opening %s: %w", repoDir, err)
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
		journalFile := filepath.Join(repoDir, fileName)
		if _, err := os.Stat(journalFile); os.IsNotExist(err) {
			log.Printf("Creating %s...\n", fileName)
			if err := os.WriteFile(journalFile, []byte(""), 0o644); err != nil {
				log.Printf("Error creating %s: %v", fileName, err)
			}
		}
	}
	return nil
}

// ensureStorage gets the storage directory ready to be written into. It must be
// called with storageMutex held.
//
// With git storage on, the directory is the cloned repo. When the clone at
// startup fails (an expired token, the network not up yet as the machine boots)
// go-git deletes the half-made directory, so the clone is retried here rather
// than leaving every write to fail against a directory that isn't there.
func ensureStorage() error {
	if !gitEnabled() {
		return os.MkdirAll(storageRoot(), 0o755)
	}
	if _, err := git.PlainOpen(repoDir); err == git.ErrRepositoryNotExists {
		if err := initGitRepo(); err != nil {
			return fmt.Errorf("storage repo unavailable: %w", err)
		}
	}
	return nil
}

func syncGit(message string) {
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
	_, err = w.Commit(message, &git.CommitOptions{
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

// gitEnabled reports whether entries are backed by the storage repo.
func gitEnabled() bool {
	return gitUsername != "" && gitRepoName != "" && githubToken != ""
}

// storageRoot is the directory journal files and photo attachments live in.
func storageRoot() string {
	if gitUsername != "" && gitRepoName != "" {
		return repoDir
	}
	return "."
}

func journalPath(entryType string) string {
	config, ok := EntryTypes[entryType]
	if !ok {
		config = EntryTypes["journal"]
	}
	return filepath.Join(storageRoot(), getJournalFileName(config.TargetFile))
}

func readJournal(entryType string) (string, error) {
	contents, err := os.ReadFile(journalPath(entryType))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return string(contents), nil
}

// writeFileAtomic writes through a temp file so a crash mid-write can't
// truncate the journal.
func writeFileAtomic(path string, data []byte) error {
	tmpFile := path + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0o644); err != nil {
		log.Printf("Error writing to %s: %v", tmpFile, err)
		return err
	}
	if err := os.Rename(tmpFile, path); err != nil {
		log.Printf("Error replacing %s: %v", path, err)
		return err
	}
	return nil
}

// indexHeader returns the offset of the line that is exactly header, or -1.
// Matching whole lines keeps "### Raw Input" from matching inside
// "#### Raw Input".
func indexHeader(block string, header string) int {
	for search := 0; search < len(block); {
		idx := strings.Index(block[search:], header)
		if idx == -1 {
			return -1
		}
		abs := search + idx
		end := abs + len(header)
		atLineStart := abs == 0 || block[abs-1] == '\n'
		atLineEnd := end == len(block) || block[end] == '\n' || block[end] == '\r'
		if atLineStart && atLineEnd {
			return abs
		}
		search = end
	}
	return -1
}

// ensureTrailingBlank normalises the end of a block so whatever follows starts
// on its own line, with a blank line in between for Markdown.
func ensureTrailingBlank(block string) string {
	if strings.TrimSpace(block) == "" {
		return block
	}
	block = strings.TrimRight(block, "\n")
	if journalFormat == "markdown" {
		return block + "\n\n"
	}
	return block + "\n"
}

// splitDateEntry returns everything before the day's entry, the entry block
// itself, and everything after it. The block is created when the day has no
// entry yet. The block always ends with a newline; the remainder starts at the
// next day's header.
func splitDateEntry(content string, dateHeader string) (before string, block string, after string) {
	idx := indexHeader(content, dateHeader)
	if idx == -1 {
		newBlock := dateHeader + "\n"
		if journalFormat == "markdown" {
			newBlock += "\n"
		}
		return ensureTrailingBlank(content), newBlock, ""
	}

	before = content[:idx]
	rest := content[idx:]
	nextIdx := strings.Index(rest[len(dateHeader):], getTopLevelHeaderPattern())
	if nextIdx == -1 {
		return before, rest, ""
	}
	// Keep the newline that ends the block with the block itself.
	split := len(dateHeader) + nextIdx + 1
	return before, rest[:split], rest[split:]
}

// splitTopics separates the part of a day's entry that is not grouped under a
// topic from the topic blocks that follow it.
func splitTopics(entryBlock string) (ungrouped string, topics string) {
	marker := "\n" + getTopicHeaderPrefix() + topicHeaderLabel
	idx := strings.Index(entryBlock, marker)
	if idx == -1 {
		return entryBlock, ""
	}
	return entryBlock[:idx+1], entryBlock[idx+1:]
}

// findTopicBlock locates the block belonging to a topic inside a day's entry.
func findTopicBlock(entryBlock string, header string) (start int, end int, found bool) {
	start = indexHeader(entryBlock, header)
	if start == -1 {
		return 0, 0, false
	}

	rest := entryBlock[start+len(header):]
	next := -1
	for _, pattern := range []string{getSectionHeaderPattern(), getTopLevelHeaderPattern()} {
		if i := strings.Index(rest, pattern); i != -1 && (next == -1 || i < next) {
			next = i
		}
	}
	if next == -1 {
		return start, len(entryBlock), true
	}
	return start, start + len(header) + next + 1, true
}

// topicsIn lists the topics recorded inside a day's entry, in file order.
func topicsIn(entryBlock string) []string {
	prefix := getTopicHeaderPrefix() + topicHeaderLabel
	var topics []string
	for _, line := range strings.Split(entryBlock, "\n") {
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		if name := strings.TrimSpace(strings.TrimPrefix(line, prefix)); name != "" {
			topics = append(topics, name)
		}
	}
	return topics
}

// sectionBody returns the text of a section, without its header.
func sectionBody(block string, sectionHeader string, level sectionLevel) string {
	idx := indexHeader(block, sectionHeader)
	if idx == -1 {
		return ""
	}
	rest := block[idx+len(sectionHeader):]
	if next := strings.Index(rest, level.pattern); next != -1 {
		rest = rest[:next]
	}
	return strings.TrimSpace(rest)
}

// insertionPoint returns where a new section should be inserted so the trailing
// sections stay at the bottom, or -1 to append at the end of the block.
func insertionPoint(block string, level sectionLevel, name string) int {
	start := 0
	for i, section := range trailingSections {
		if section == name {
			start = i + 1
			break
		}
	}

	best := -1
	for _, section := range trailingSections[start:] {
		if idx := indexHeader(block, level.prefix+section); idx != -1 && (best == -1 || idx < best) {
			best = idx
		}
	}
	return best
}

func appendToSection(block string, sectionHeader string, newItem string, level sectionLevel) string {
	idx := indexHeader(block, sectionHeader)
	if idx == -1 {
		// Section missing
		separator := "\n"
		if journalFormat == "markdown" {
			separator = "\n\n"
		}
		section := sectionHeader + separator + newItem + "\n"

		if at := insertionPoint(block, level, strings.TrimPrefix(sectionHeader, level.prefix)); at != -1 {
			if journalFormat == "markdown" {
				section += "\n"
			}
			return block[:at] + section + block[at:]
		}

		if !strings.HasSuffix(block, "\n") {
			block += "\n"
		}
		return block + section
	}

	// Section exists: append to its body, keeping the blank line that
	// separates the section from whatever follows it.
	bodyStart := idx + len(sectionHeader)
	bodyEnd := len(block)
	if nextSectionIdx := strings.Index(block[bodyStart:], level.pattern); nextSectionIdx != -1 {
		bodyEnd = bodyStart + nextSectionIdx
	}

	body := strings.TrimRight(block[bodyStart:bodyEnd], "\n") + "\n" + newItem
	if bodyEnd == len(block) || journalFormat == "markdown" {
		// The next section's pattern already starts with a newline, so for
		// Markdown this second one is the blank line before that header.
		body += "\n"
	}
	return block[:bodyStart] + body + block[bodyEnd:]
}

// removeSection drops a section, header and body, so it can be rewritten.
func removeSection(block string, sectionHeader string, level sectionLevel) string {
	idx := indexHeader(block, sectionHeader)
	if idx == -1 {
		return block
	}

	rest := block[idx+len(sectionHeader):]
	next := strings.Index(rest, level.pattern)
	if next == -1 {
		return ensureTrailingBlank(block[:idx])
	}
	return block[:idx] + rest[next+1:]
}

func headerFor(field string) string {
	if header, ok := HeaderMapping[field]; ok {
		return header
	}
	return field
}

// renderValue turns an analysis value into the lines that represent it.
func renderValue(val interface{}) []string {
	switch v := val.(type) {
	case string:
		return []string{v}
	case []interface{}:
		lines := make([]string, 0, len(v))
		for _, item := range v {
			lines = append(lines, fmt.Sprintf("- %v", item))
		}
		return lines
	case []string:
		lines := make([]string, 0, len(v))
		for _, item := range v {
			lines = append(lines, "- "+item)
		}
		return lines
	}
	return nil
}

// applyAnalysis writes the analysis fields into a block. When replace is set the
// configured fields are cleared first, so a re-synthesis of a topic supersedes
// the previous one instead of stacking on top of it.
func applyAnalysis(entryType string, block string, analysis map[string]interface{}, level sectionLevel, replace bool) string {
	config, ok := EntryTypes[entryType]
	if !ok {
		config = EntryTypes["journal"]
	}

	if replace {
		for _, field := range config.Fields {
			block = removeSection(block, level.prefix+headerFor(field), level)
		}
	}

	for _, field := range config.Fields {
		val, ok := analysis[field]
		if !ok {
			continue
		}
		sectionHeader := level.prefix + headerFor(field)
		for _, line := range renderValue(val) {
			block = appendToSection(block, sectionHeader, line, level)
		}
	}

	for _, section := range trailingSections {
		if val, ok := analysis[section].(string); ok && val != "" {
			block = appendToSection(block, level.prefix+section, val, level)
		}
	}

	return block
}

// applyToEntry runs mutate over the region of the journal an entry belongs to:
// the topic block when a topic is set, otherwise the ungrouped part of the day's
// entry. Both regions are created when they don't exist yet.
func applyToEntry(content string, dateHeader string, topic string, mutate func(block string, level sectionLevel) string) string {
	before, entryBlock, after := splitDateEntry(content, dateHeader)

	if topic == "" {
		ungrouped, topics := splitTopics(entryBlock)
		ungrouped = mutate(ungrouped, dateSectionLevel())
		if topics != "" {
			ungrouped = ensureTrailingBlank(ungrouped)
		}
		entryBlock = ungrouped + topics
	} else {
		header := topicHeaderLine(topic)
		start, end, found := findTopicBlock(entryBlock, header)
		if !found {
			topicBlock := header + "\n"
			if journalFormat == "markdown" {
				topicBlock += "\n"
			}
			entryBlock = ensureTrailingBlank(entryBlock) + mutate(topicBlock, topicSectionLevel())
		} else {
			topicBlock := mutate(entryBlock[start:end], topicSectionLevel())
			if entryBlock[end:] != "" {
				topicBlock = ensureTrailingBlank(topicBlock)
			}
			entryBlock = entryBlock[:start] + topicBlock + entryBlock[end:]
		}
	}

	if after != "" {
		entryBlock = ensureTrailingBlank(entryBlock)
	}
	return before + entryBlock + after
}

// updateJournal applies mutate to an entry's region of the journal file and
// persists the result.
func updateJournal(entryType string, dateHeader string, topic string, commitMsg string, mutate func(block string, level sectionLevel) string) error {
	storageMutex.Lock()
	defer storageMutex.Unlock()

	// Before reading: a journal read ahead of a late clone would come back
	// empty, and writing that back would wipe the cloned history.
	if err := ensureStorage(); err != nil {
		log.Printf("Error preparing storage: %v", err)
		return err
	}

	targetFile := journalPath(entryType)
	existingContent, err := readJournal(entryType)
	if err != nil {
		log.Printf("Error reading %s: %v", targetFile, err)
		return err
	}

	if dateHeader == "" {
		dateHeader = time.Now().Format(getDateHeaderFormat())
	}

	if err := writeFileAtomic(targetFile, []byte(applyToEntry(existingContent, dateHeader, topic, mutate))); err != nil {
		return err
	}

	if gitEnabled() {
		syncGit(commitMsg)
	}
	return nil
}

func entryCommitMessage(topic string) string {
	stamp := time.Now().Format("2006-01-02 15:04")
	if topic != "" {
		return fmt.Sprintf("Journal entry %s (%s)", stamp, topic)
	}
	return fmt.Sprintf("Journal entry %s", stamp)
}

// SaveRawEntry persists just the raw input under the given date header, and
// topic when set, so the text is durable before (and regardless of) AI
// processing.
func SaveRawEntry(entryType string, content string, dateHeader string, topic string) error {
	return SaveEntry(entryType, map[string]interface{}{"Raw Input": content}, dateHeader, topic)
}

// SaveEntry merges analysis sections into an entry, appending to any sections
// that are already there.
func SaveEntry(entryType string, analysis map[string]interface{}, dateHeader string, topic string) error {
	return updateJournal(entryType, dateHeader, topic, entryCommitMessage(topic), func(block string, level sectionLevel) string {
		return applyAnalysis(entryType, block, analysis, level, false)
	})
}

// ReplaceTopicAnalysis rewrites a topic's analysis sections so the synthesis
// covers the whole topic, rather than accumulating one summary per note. The
// topic's photos and raw notes are left untouched.
func ReplaceTopicAnalysis(entryType string, analysis map[string]interface{}, dateHeader string, topic string) error {
	msg := fmt.Sprintf("Synthesis for %s (%s)", topic, time.Now().Format("2006-01-02 15:04"))
	return updateJournal(entryType, dateHeader, topic, msg, func(block string, level sectionLevel) string {
		return applyAnalysis(entryType, block, analysis, level, true)
	})
}

// SavePhotoReference records an attached photo in an entry's Photos section.
func SavePhotoReference(entryType string, dateHeader string, topic string, reference string) error {
	msg := fmt.Sprintf("Photo %s", time.Now().Format("2006-01-02 15:04"))
	if topic != "" {
		msg = fmt.Sprintf("Photo for %s (%s)", topic, time.Now().Format("2006-01-02 15:04"))
	}
	return updateJournal(entryType, dateHeader, topic, msg, func(block string, level sectionLevel) string {
		return appendToSection(block, level.prefix+"Photos", reference, level)
	})
}

// ListTopics returns the topics already recorded under a date header, so the UI
// can offer them instead of making the author retype a talk title.
func ListTopics(entryType string, dateHeader string) ([]string, error) {
	storageMutex.RLock()
	defer storageMutex.RUnlock()

	content, err := readJournal(entryType)
	if err != nil {
		return nil, err
	}
	if indexHeader(content, dateHeader) == -1 {
		return nil, nil
	}

	_, entryBlock, _ := splitDateEntry(content, dateHeader)
	return topicsIn(entryBlock), nil
}

// GetTopicNotes returns the raw notes accumulated so far under a topic, so the
// synthesis can summarise the session as a whole.
func GetTopicNotes(entryType string, dateHeader string, topic string) (string, error) {
	storageMutex.RLock()
	defer storageMutex.RUnlock()

	content, err := readJournal(entryType)
	if err != nil {
		return "", err
	}
	if indexHeader(content, dateHeader) == -1 {
		return "", nil
	}

	_, entryBlock, _ := splitDateEntry(content, dateHeader)
	start, end, found := findTopicBlock(entryBlock, topicHeaderLine(topic))
	if !found {
		return "", nil
	}

	level := topicSectionLevel()
	return sectionBody(entryBlock[start:end], level.prefix+"Raw Input", level), nil
}

func GetEntries(entryType string) (string, error) {
	storageMutex.RLock()
	defer storageMutex.RUnlock()

	return readJournal(entryType)
}
