package conversationstenography

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
)

// PythonTokenizer implements the Tokenizer interface using a Python subprocess
// that loads only the HuggingFace tokenizer (not the model).
type PythonTokenizer struct {
	cmd         *exec.Cmd
	in          io.WriteCloser
	out         *bufio.Reader
	fingerprint string
	mu          sync.Mutex
}

type tokenizerRequest struct {
	Op          string   `json:"op"`
	Text        string   `json:"text,omitempty"`
	Tokens      []int    `json:"tokens,omitempty"`
	TokenStrs   []string `json:"token_strings,omitempty"`
}

type tokenizerResponse struct {
	OK          bool     `json:"ok"`
	Error       string   `json:"error,omitempty"`
	Fingerprint string   `json:"fingerprint,omitempty"`
	Text        string   `json:"text,omitempty"`
	Tokens      []int    `json:"tokens,omitempty"`
	IDs         []int    `json:"ids,omitempty"`
}

// NewPythonTokenizer starts a Python tokenizer process.
func NewPythonTokenizer(ctx context.Context, python, model, revision string) (*PythonTokenizer, error) {
	cmd := exec.CommandContext(ctx, python, "-u", "-m", "python.tokenizer_backend",
		"--model", model, "--revision", revision)
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("tokenizer stdin: %w", err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("tokenizer stdout: %w", err)
	}
	var stderrBuf bytesBuffer
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("tokenizer start: %w", err)
	}

	t := &PythonTokenizer{
		cmd: cmd,
		in:  in,
		out: bufio.NewReader(out),
	}

	resp, err := t.call(tokenizerRequest{Op: "info"})
	if err != nil {
		_ = t.Close()
		if stderrBuf.String() != "" {
			return nil, fmt.Errorf("tokenizer init: %w (stderr: %s)", err, stderrBuf.String())
		}
		return nil, fmt.Errorf("tokenizer init: %w", err)
	}
	if resp.Fingerprint == "" {
		_ = t.Close()
		return nil, fmt.Errorf("tokenizer did not return a fingerprint")
	}

	t.fingerprint = resp.Fingerprint
	return t, nil
}

func (t *PythonTokenizer) Fingerprint() string { return t.fingerprint }

func (t *PythonTokenizer) Tokenize(_ context.Context, text string) ([]int, error) {
	resp, err := t.call(tokenizerRequest{Op: "tokenize", Text: text})
	if err != nil {
		return nil, err
	}
	return resp.Tokens, nil
}

func (t *PythonTokenizer) Detokenize(_ context.Context, ids []int) (string, error) {
	resp, err := t.call(tokenizerRequest{Op: "detokenize", Tokens: ids})
	if err != nil {
		return "", err
	}
	return resp.Text, nil
}

// TokensToIDs maps token strings back to their vocabulary IDs using the
// tokenizer's internal vocabulary. This is the correct reverse mapping
// (unlike calling Tokenize on a single-token string, which may split it).
func (t *PythonTokenizer) TokensToIDs(_ context.Context, tokens []string) ([]int, error) {
	resp, err := t.call(tokenizerRequest{Op: "tokens_to_ids", TokenStrs: tokens})
	if err != nil {
		return nil, err
	}
	return resp.IDs, nil
}

func (t *PythonTokenizer) call(req tokenizerRequest) (*tokenizerResponse, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	reqBytes, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("tokenizer marshal: %w", err)
	}

	reqBytes = append(reqBytes, '\n')
	if _, err := t.in.Write(reqBytes); err != nil {
		return nil, fmt.Errorf("tokenizer write: %w", err)
	}

	line, err := t.out.ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("tokenizer read: %w", err)
	}

	var resp tokenizerResponse
	if err := json.Unmarshal(line, &resp); err != nil {
		return nil, fmt.Errorf("tokenizer parse: %w (raw: %s)", err, string(line))
	}
	if !resp.OK {
		return nil, fmt.Errorf("tokenizer error: %s", resp.Error)
	}
	return &resp, nil
}

// Close terminates the tokenizer process.
func (t *PythonTokenizer) Close() error {
	t.in.Close()
	return t.cmd.Wait()
}


