package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

var version = "dev"

const apiURL = "https://api.anthropic.com/v1/messages"

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type request struct {
	Model     string    `json:"model"`
	MaxTokens int       `json:"max_tokens"`
	System    string    `json:"system,omitempty"`
	Stream    bool      `json:"stream"`
	Messages  []message `json:"messages"`
}

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Println("yoloclaude", version)
		return
	}

	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "Error: ANTHROPIC_API_KEY environment variable is required")
		os.Exit(1)
	}

	model := os.Getenv("YOLO_MODEL")
	if model == "" {
		model = "claude-sonnet-4-6"
	}

	systemPrompt := os.Getenv("YOLO_SYSTEM_PROMPT")
	if systemPrompt == "" {
		systemPrompt = "You are a helpful coding assistant. Be concise and direct."
	}

	// One-shot mode: pass prompt as argument
	if len(os.Args) > 1 {
		prompt := strings.Join(os.Args[1:], " ")
		resp, err := sendMessage(apiKey, model, systemPrompt, []message{{Role: "user", Content: prompt}})
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
		fmt.Println(resp)
		return
	}

	// Interactive mode
	fmt.Printf("yoloclaude %s (model: %s)\nType 'exit' to quit.\n\n", version, model)
	history := []message{}
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			break
		}
		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}
		if input == "exit" || input == "quit" {
			break
		}
		if input == "/clear" {
			history = history[:0]
			fmt.Println("History cleared.")
			continue
		}

		history = append(history, message{Role: "user", Content: input})

		resp, err := streamMessage(apiKey, model, systemPrompt, history)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			// Remove failed message from history
			history = history[:len(history)-1]
			continue
		}

		history = append(history, message{Role: "assistant", Content: resp})
		fmt.Println()
	}
}

func sendMessage(apiKey, model, system string, messages []message) (string, error) {
	body, _ := json.Marshal(request{
		Model:     model,
		MaxTokens: 4096,
		System:    system,
		Stream:    false,
		Messages:  messages,
	})

	req, _ := http.NewRequest("POST", apiURL, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("API error %d: %s", resp.StatusCode, string(data))
	}

	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	json.Unmarshal(data, &result)
	if len(result.Content) == 0 {
		return "", fmt.Errorf("empty response")
	}
	return result.Content[0].Text, nil
}

func streamMessage(apiKey, model, system string, messages []message) (string, error) {
	body, _ := json.Marshal(request{
		Model:     model,
		MaxTokens: 4096,
		System:    system,
		Stream:    true,
		Messages:  messages,
	})

	req, _ := http.NewRequest("POST", apiURL, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		data, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("API error %d: %s", resp.StatusCode, string(data))
	}

	var full strings.Builder
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}
		var event struct {
			Type  string `json:"type"`
			Delta struct {
				Text string `json:"text"`
			} `json:"delta"`
		}
		if json.Unmarshal([]byte(data), &event) == nil && event.Type == "content_block_delta" {
			fmt.Print(event.Delta.Text)
			full.WriteString(event.Delta.Text)
		}
	}
	return full.String(), nil
}
