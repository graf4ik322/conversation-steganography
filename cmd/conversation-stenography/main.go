package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

const banner = `
  ╔══════════════════════════════════════════════════╗
  ║       🔒  Conversation Stenography — hidden chat   ║
  ║     Hide secret messages in normal-looking text  ║
  ╚══════════════════════════════════════════════════╝
`

const usage = `Conversation Stenography — hide secret messages inside natural chat text

Usage:
  conversation-stenography                    First-run setup or start chatting
  conversation-stenography chat               Start a secure conversation
  conversation-stenography simulate           Simulate two people on this device
  conversation-stenography conversations      List saved conversations
  conversation-stenography setup              Re-run the setup wizard

Advanced:
  conversation-stenography chain-send    -from NAME [-state FILE] < plaintext > record.json
  conversation-stenography chain-receive [-state FILE] < record.json > plaintext
  conversation-stenography chain-show    [-state FILE]
  conversation-stenography chain-chat    -as NAME [-state FILE]

Getting started:
  Just run "conversation-stenography" with no arguments — it will walk you through everything.
  Type /quit or press Ctrl-D to leave any interactive mode.`

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string, in io.Reader, out, errOut io.Writer) error {
	if len(args) == 0 {
		return runDefault(in, out, errOut)
	}

	mode := args[0]
	if mode == "help" || mode == "-h" || mode == "--help" {
		fmt.Fprintln(out, usage)
		return nil
	}

	switch mode {
	case "setup":
		return runSetupWizard(in, out, errOut)
	case "chat":
		return runQuickChat(args[1:], in, out, errOut)
	case "simulate":
		return runSimulation(args[1:], in, out, errOut)
	case "conversations":
		if len(args) != 1 {
			return errors.New("conversations takes no arguments")
		}
		return listConversations(out)
	case "chain-send", "chain-receive", "chain-show", "chain-chat":
		return runChain(mode, args[1:], in, out, errOut)
	default:
		return fmt.Errorf("unknown command %q — run \"conversation-stenography help\" to see available commands", mode)
	}
}

// runDefault is what happens when you run the CLI with no arguments.
// If no config exists, it launches the setup wizard. If config exists, it
// launches interactive chat mode.
func runDefault(in io.Reader, out, errOut io.Writer) error {
	configPath := resolveSupportFile("conversation-stenography.local.json")
	if _, err := os.Stat(configPath); errors.Is(err, os.ErrNotExist) {
		fmt.Fprint(out, banner)
		fmt.Fprintln(out, "  Welcome! Let's get you set up.")
		fmt.Fprintln(out)
		return runSetupWizard(in, out, errOut)
	}
	// Config exists — go straight to chat
	fmt.Fprint(out, banner)
	return runQuickChat(nil, in, out, errOut)
}

// runQuickChat is a friendlier wrapper around the chat subcommand. If no flags
// are given, it prompts interactively for conversation name and your name.
func runQuickChat(args []string, in io.Reader, out, errOut io.Writer) error {
	// If flags are provided, fall through to the normal chain handler
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			return runChain("chat", args, in, out, errOut)
		}
	}

	// Interactive prompt for chat parameters
	scanner := bufio.NewScanner(in)

	fmt.Fprintln(out, "  ┌─────────────────────────────────────┐")
	fmt.Fprintln(out, "  │          Start a conversation        │")
	fmt.Fprintln(out, "  └─────────────────────────────────────┘")
	fmt.Fprintln(out)

	fmt.Fprintln(out, "  What conversation do you want to join?")
	fmt.Fprintln(out, "  Both people must use the exact same name (case-sensitive).")
	fmt.Fprint(out, "\n  Conversation name: ")
	if !scanner.Scan() {
		return scanner.Err()
	}
	conversation := strings.TrimSpace(scanner.Text())
	if conversation == "" {
		conversation = "default-chat"
		fmt.Fprintf(out, "  → Using %q\n", conversation)
	}

	fmt.Fprintln(out)
	fmt.Fprintln(out, "  What's your name in this conversation?")
	fmt.Fprint(out, "\n  Your name: ")
	if !scanner.Scan() {
		return scanner.Err()
	}
	me := strings.TrimSpace(scanner.Text())
	if me == "" {
		return errors.New("your name cannot be empty")
	}

	fmt.Fprintln(out)
	return runChain("chat", []string{"-conversation", conversation, "-me", me}, in, out, errOut)
}
