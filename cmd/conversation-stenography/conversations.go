package main

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// defaultConversationState, listConversations, and safeStateName manage the
// per-conversation state files stored under
// ~/.conversation-stenography/conversations/ for the "chat" subcommand.

func defaultConversationState(conversation, me string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find home directory: %w", err)
	}
	digest := sha256.Sum256([]byte(conversation + "\x00" + me))
	name := safeStateName(conversation) + "--" + safeStateName(me) + "--" + fmt.Sprintf("%x", digest[:5]) + ".json"
	renamed := filepath.Join(home, ".conversation-stenography", "conversations", name)
	legacy := filepath.Join(home, ".decalgo", "conversations", name)
	if _, err := os.Stat(renamed); errors.Is(err, os.ErrNotExist) {
		if _, legacyErr := os.Stat(legacy); legacyErr == nil {
			return legacy, nil
		}
	}
	return renamed, nil
}

func listConversations(out io.Writer) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	count := 0
	seen := make(map[string]bool)
	for _, directory := range []string{
		filepath.Join(home, ".conversation-stenography", "conversations"),
		filepath.Join(home, ".decalgo", "conversations"),
	} {
		entries, err := os.ReadDir(directory)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("list conversations: %w", err)
		}
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" || seen[entry.Name()] {
				continue
			}
			fmt.Fprintln(out, strings.TrimSuffix(entry.Name(), ".json"))
			seen[entry.Name()] = true
			count++
		}
	}
	if count == 0 {
		fmt.Fprintln(out, "No local conversations yet.")
	}
	return nil
}

func safeStateName(value string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(value) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		} else if b.Len() > 0 && !strings.HasSuffix(b.String(), "-") {
			b.WriteByte('-')
		}
		if b.Len() >= 32 {
			break
		}
	}
	name := strings.Trim(b.String(), "-")
	if name == "" {
		return "conversation"
	}
	return name
}
