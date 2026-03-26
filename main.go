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

const (
	anthropicURL = "https://api.anthropic.com/v1/messages"
	ollamaURL    = "http://localhost:11434"
)

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Anthropic API types
type anthropicRequest struct {
	Model     string    `json:"model"`
	MaxTokens int       `json:"max_tokens"`
	System    string    `json:"system,omitempty"`
	Stream    bool      `json:"stream"`
	Messages  []message `json:"messages"`
}

// Ollama API types
type ollamaRequest struct {
	Model    string           `json:"model"`
	Messages []ollamaMessage  `json:"messages"`
	Stream   bool             `json:"stream"`
}

type ollamaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type config struct {
	APIKey       string `json:"api_key"`
	Model        string `json:"model"`
	SystemPrompt string `json:"system_prompt"`
	OllamaURL    string `json:"ollama_url,omitempty"`
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

func isOllamaModel(model string) bool {
	return strings.HasPrefix(model, "ollama:")
}

func ollamaModelName(model string) string {
	return strings.TrimPrefix(model, "ollama:")
}

func setupWizard(scanner *bufio.Scanner) *config {
	fmt.Println("=== YoloClaude - First time setup ===")
	fmt.Println()
	fmt.Println("Choose your provider:")
	fmt.Println("  1) Anthropic API (Claude)")
	fmt.Println("  2) Ollama (local models)")
	fmt.Print("Choice [1]: ")
	scanner.Scan()
	provider := strings.TrimSpace(scanner.Text())

	cfg := &config{
		SystemPrompt: "You are a helpful coding assistant. Be concise and direct.",
	}

	if provider == "2" {
		fmt.Println()
		fmt.Print("Ollama URL [http://localhost:11434]: ")
		scanner.Scan()
		url := strings.TrimSpace(scanner.Text())
		if url == "" {
			url = ollamaURL
		}
		cfg.OllamaURL = url

		fmt.Print("Model name (e.g. llama3, codellama, mistral): ")
		scanner.Scan()
		model := strings.TrimSpace(scanner.Text())
		if model == "" {
			model = "llama3"
		}
		cfg.Model = "ollama:" + model
	} else {
		fmt.Println()
		fmt.Print("Enter your Anthropic API key: ")
		scanner.Scan()
		cfg.APIKey = strings.TrimSpace(scanner.Text())

		fmt.Println()
		fmt.Println("Choose a model:")
		fmt.Println("  1) claude-sonnet-4-6 (default, fast)")
		fmt.Println("  2) claude-opus-4-6 (most capable)")
		fmt.Println("  3) claude-haiku-4-5 (fastest, cheapest)")
		fmt.Print("Choice [1]: ")
		scanner.Scan()
		switch strings.TrimSpace(scanner.Text()) {
		case "2":
			cfg.Model = "claude-opus-4-6"
		case "3":
			cfg.Model = "claude-haiku-4-5-20251001"
		default:
			cfg.Model = "claude-sonnet-4-6"
		}
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

	// Parse --model flag
	var flagModel string
	args := []string{}
	for i := 1; i < len(os.Args); i++ {
		if os.Args[i] == "--model" && i+1 < len(os.Args) {
			flagModel = os.Args[i+1]
			i++
		} else if strings.HasPrefix(os.Args[i], "--model=") {
			flagModel = strings.TrimPrefix(os.Args[i], "--model=")
		} else {
			args = append(args, os.Args[i])
		}
	}

	// Load or create config
	cfg, err := loadConfig()
	if err != nil || (cfg.APIKey == "" && !isOllamaModel(cfg.Model)) {
		if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
			cfg = &config{
				APIKey:       key,
				Model:        "claude-sonnet-4-6",
				SystemPrompt: "You are a helpful coding assistant. Be concise and direct.",
			}
		} else if flagModel != "" && isOllamaModel("ollama:"+flagModel) {
			// Direct ollama usage without setup
			cfg = &config{
				Model:        "ollama:" + flagModel,
				OllamaURL:    ollamaURL,
				SystemPrompt: "You are a helpful coding assistant. Be concise and direct.",
			}
		} else {
			cfg = setupWizard(scanner)
		}
	}

	// --model flag overrides config
	if flagModel != "" {
		if !strings.Contains(flagModel, "claude") && !strings.HasPrefix(flagModel, "ollama:") {
			// Assume it's an ollama model
			cfg.Model = "ollama:" + flagModel
			if cfg.OllamaURL == "" {
				cfg.OllamaURL = ollamaURL
			}
		} else {
			cfg.Model = flagModel
		}
	}

	// Override from env
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		cfg.APIKey = key
	}
	if model := os.Getenv("YOLO_MODEL"); model != "" {
		cfg.Model = model
	}
	if url := os.Getenv("OLLAMA_URL"); url != "" {
		cfg.OllamaURL = url
	}

	// One-shot mode
	if len(args) > 0 {
		prompt := strings.Join(args, " ")
		var resp string
		var err error
		if isOllamaModel(cfg.Model) {
			resp, err = sendOllama(cfg, []message{{Role: "user", Content: prompt}})
		} else {
			resp, err = sendAnthropic(cfg, []message{{Role: "user", Content: prompt}})
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
		fmt.Println(resp)
		return
	}

	// Interactive mode
	displayModel := cfg.Model
	if isOllamaModel(cfg.Model) {
		displayModel = ollamaModelName(cfg.Model) + " (ollama)"
	}
	fmt.Printf("yoloclaude %s (model: %s)\n", version, displayModel)
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
			fmt.Println("  4) ollama model (enter name)")
			fmt.Print("Choice: ")
			if scanner.Scan() {
				choice := strings.TrimSpace(scanner.Text())
				switch choice {
				case "1":
					cfg.Model = "claude-sonnet-4-6"
				case "2":
					cfg.Model = "claude-opus-4-6"
				case "3":
					cfg.Model = "claude-haiku-4-5-20251001"
				case "4":
					fmt.Print("Ollama model name: ")
					if scanner.Scan() {
						cfg.Model = "ollama:" + strings.TrimSpace(scanner.Text())
						if cfg.OllamaURL == "" {
							cfg.OllamaURL = ollamaURL
						}
					}
				default:
					// Treat as direct ollama model name
					cfg.Model = "ollama:" + choice
					if cfg.OllamaURL == "" {
						cfg.OllamaURL = ollamaURL
					}
				}
				saveConfig(cfg)
				fmt.Printf("Model set to %s\n", cfg.Model)
			}
			continue
		}
		if input == "/config" {
			fmt.Printf("Config: %s\n", configPath())
			fmt.Printf("Model:  %s\n", cfg.Model)
			if isOllamaModel(cfg.Model) {
				fmt.Printf("Ollama: %s\n", cfg.OllamaURL)
			} else if len(cfg.APIKey) > 8 {
				fmt.Printf("API Key: %s...%s\n", cfg.APIKey[:4], cfg.APIKey[len(cfg.APIKey)-4:])
			}
			continue
		}

		history = append(history, message{Role: "user", Content: input})

		var resp string
		if isOllamaModel(cfg.Model) {
			resp, err = streamOllama(cfg, history)
		} else {
			resp, err = streamAnthropic(cfg, history)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			history = history[:len(history)-1]
			continue
		}

		history = append(history, message{Role: "assistant", Content: resp})
		fmt.Println()
	}
}

// ---- Anthropic ----

func sendAnthropic(cfg *config, messages []message) (string, error) {
	body, _ := json.Marshal(anthropicRequest{
		Model:     cfg.Model,
		MaxTokens: 4096,
		System:    cfg.SystemPrompt,
		Stream:    false,
		Messages:  messages,
	})

	req, _ := http.NewRequest("POST", anthropicURL, bytes.NewReader(body))
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

func streamAnthropic(cfg *config, messages []message) (string, error) {
	body, _ := json.Marshal(anthropicRequest{
		Model:     cfg.Model,
		MaxTokens: 4096,
		System:    cfg.SystemPrompt,
		Stream:    true,
		Messages:  messages,
	})

	req, _ := http.NewRequest("POST", anthropicURL, bytes.NewReader(body))
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

// ---- Ollama ----

func sendOllama(cfg *config, messages []message) (string, error) {
	ollamaMsgs := make([]ollamaMessage, 0, len(messages)+1)
	if cfg.SystemPrompt != "" {
		ollamaMsgs = append(ollamaMsgs, ollamaMessage{Role: "system", Content: cfg.SystemPrompt})
	}
	for _, m := range messages {
		ollamaMsgs = append(ollamaMsgs, ollamaMessage{Role: m.Role, Content: m.Content})
	}

	body, _ := json.Marshal(ollamaRequest{
		Model:    ollamaModelName(cfg.Model),
		Messages: ollamaMsgs,
		Stream:   false,
	})

	url := cfg.OllamaURL
	if url == "" {
		url = ollamaURL
	}

	resp, err := http.Post(url+"/api/chat", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("cannot connect to Ollama at %s: %v", url, err)
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("Ollama error %d: %s", resp.StatusCode, string(data))
	}

	var result struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	json.Unmarshal(data, &result)
	return result.Message.Content, nil
}

func streamOllama(cfg *config, messages []message) (string, error) {
	ollamaMsgs := make([]ollamaMessage, 0, len(messages)+1)
	if cfg.SystemPrompt != "" {
		ollamaMsgs = append(ollamaMsgs, ollamaMessage{Role: "system", Content: cfg.SystemPrompt})
	}
	for _, m := range messages {
		ollamaMsgs = append(ollamaMsgs, ollamaMessage{Role: m.Role, Content: m.Content})
	}

	body, _ := json.Marshal(ollamaRequest{
		Model:    ollamaModelName(cfg.Model),
		Messages: ollamaMsgs,
		Stream:   true,
	})

	url := cfg.OllamaURL
	if url == "" {
		url = ollamaURL
	}

	resp, err := http.Post(url+"/api/chat", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("cannot connect to Ollama at %s: %v", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		data, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("Ollama error %d: %s", resp.StatusCode, string(data))
	}

	var full strings.Builder
	decoder := json.NewDecoder(resp.Body)
	for decoder.More() {
		var chunk struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			Done bool `json:"done"`
		}
		if err := decoder.Decode(&chunk); err != nil {
			break
		}
		fmt.Print(chunk.Message.Content)
		full.WriteString(chunk.Message.Content)
		if chunk.Done {
			break
		}
	}
	return full.String(), nil
}
