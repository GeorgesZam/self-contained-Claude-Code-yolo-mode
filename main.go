package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
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

type config struct {
	APIKey       string `json:"api_key"`
	Model        string `json:"model"`
	SystemPrompt string `json:"system_prompt"`
}

func configDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".yoloclaude")
}

func configPath() string {
	return filepath.Join(configDir(), "config.json")
}

func loadConfig() (*config, error) {
	data, err := os.ReadFile(configPath())
	if err != nil {
		return nil, err
	}
	var cfg config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func saveConfig(cfg *config) error {
	os.MkdirAll(configDir(), 0700)
	data, _ := json.MarshalIndent(cfg, "", "  ")
	return os.WriteFile(configPath(), data, 0600)
}

func setupWizard(scanner *bufio.Scanner) *config {
	fmt.Println("=== YoloClaude - First time setup ===")
	fmt.Println()

	fmt.Print("Enter your Anthropic API key: ")
	scanner.Scan()
	apiKey := strings.TrimSpace(scanner.Text())

	fmt.Println()
	fmt.Println("Choose a model:")
	fmt.Println("  1) claude-sonnet-4-6 (default, fast)")
	fmt.Println("  2) claude-opus-4-6 (most capable)")
	fmt.Println("  3) claude-haiku-4-5 (fastest, cheapest)")
	fmt.Print("Choice [1]: ")
	scanner.Scan()
	choice := strings.TrimSpace(scanner.Text())

	model := "claude-sonnet-4-6"
	switch choice {
	case "2":
		model = "claude-opus-4-6"
	case "3":
		model = "claude-haiku-4-5-20251001"
	}

	cfg := &config{
		APIKey:       apiKey,
		Model:        model,
		SystemPrompt: "You are a helpful coding assistant. Be concise and direct.",
	}

	if err := saveConfig(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not save config: %v\n", err)
	} else {
		fmt.Printf("\nConfig saved to %s\n", configPath())
	}

	fmt.Println()
	return cfg
}

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Println("yoloclaude", version)
		return
	}

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	// Load or create config
	cfg, err := loadConfig()
	if err != nil || cfg.APIKey == "" {
		// Check env var as fallback
		if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
			cfg = &config{
				APIKey:       key,
				Model:        "claude-sonnet-4-6",
				SystemPrompt: "You are a helpful coding assistant. Be concise and direct.",
			}
		} else {
			cfg = setupWizard(scanner)
		}
	}

	// Override from env if set
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		cfg.APIKey = key
	}
	if model := os.Getenv("YOLO_MODEL"); model != "" {
		cfg.Model = model
	}

	// One-shot mode
	if len(os.Args) > 1 {
		prompt := strings.Join(os.Args[1:], " ")
		resp, err := sendMessage(cfg, []message{{Role: "user", Content: prompt}})
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
		fmt.Println(resp)
		return
	}

	// Interactive mode
	fmt.Printf("yoloclaude %s (model: %s)\n", version, cfg.Model)
	fmt.Println("Commands: /clear, /model, /config, exit")
	fmt.Println()

	history := []message{}

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
		if input == "/model" {
			fmt.Printf("Current model: %s\n", cfg.Model)
			fmt.Println("  1) claude-sonnet-4-6")
			fmt.Println("  2) claude-opus-4-6")
			fmt.Println("  3) claude-haiku-4-5")
			fmt.Print("Choice: ")
			if scanner.Scan() {
				switch strings.TrimSpace(scanner.Text()) {
				case "1":
					cfg.Model = "claude-sonnet-4-6"
				case "2":
					cfg.Model = "claude-opus-4-6"
				case "3":
					cfg.Model = "claude-haiku-4-5-20251001"
				}
				saveConfig(cfg)
				fmt.Printf("Model set to %s\n", cfg.Model)
			}
			continue
		}
		if input == "/config" {
			fmt.Printf("Config: %s\n", configPath())
			fmt.Printf("Model:  %s\n", cfg.Model)
			fmt.Printf("API Key: %s...%s\n", cfg.APIKey[:4], cfg.APIKey[len(cfg.APIKey)-4:])
			continue
		}

		history = append(history, message{Role: "user", Content: input})

		resp, err := streamMessage(cfg, history)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			history = history[:len(history)-1]
			continue
		}

		history = append(history, message{Role: "assistant", Content: resp})
		fmt.Println()
	}
}

func sendMessage(cfg *config, messages []message) (string, error) {
	body, _ := json.Marshal(request{
		Model:     cfg.Model,
		MaxTokens: 4096,
		System:    cfg.SystemPrompt,
		Stream:    false,
		Messages:  messages,
	})

	req, _ := http.NewRequest("POST", apiURL, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", cfg.APIKey)
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

func streamMessage(cfg *config, messages []message) (string, error) {
	body, _ := json.Marshal(request{
		Model:     cfg.Model,
		MaxTokens: 4096,
		System:    cfg.SystemPrompt,
		Stream:    true,
		Messages:  messages,
	})

	req, _ := http.NewRequest("POST", apiURL, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", cfg.APIKey)
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
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		line := sc.Text()
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
