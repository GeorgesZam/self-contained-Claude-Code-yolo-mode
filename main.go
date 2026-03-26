package main

import (
	"archive/zip"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

var version = "dev"

const (
	nodeVersion    = "v22.15.0"
	claudeCodePkg  = "@anthropic-ai/claude-code"
)

func dataDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".yoloclaude")
}

func nodeDir() string {
	return filepath.Join(dataDir(), "node")
}

func nodeExe() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(nodeDir(), "node.exe")
	}
	return filepath.Join(nodeDir(), "bin", "node")
}

func npxExe() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(nodeDir(), "npx.cmd")
	}
	return filepath.Join(nodeDir(), "bin", "npx")
}

func claudeDir() string {
	return filepath.Join(dataDir(), "claude-code")
}

func claudeBin() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(claudeDir(), "node_modules", ".bin", "claude.cmd")
	}
	return filepath.Join(claudeDir(), "node_modules", ".bin", "claude")
}

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--launcher-version") {
		fmt.Println("yoloclaude launcher", version)
		return
	}

	// Check if Claude Code is installed
	if _, err := os.Stat(claudeBin()); os.IsNotExist(err) {
		if err := setup(); err != nil {
			fmt.Fprintf(os.Stderr, "Setup failed: %v\n", err)
			os.Exit(1)
		}
	}

	// Run Claude Code, passing through all args, stdin, stdout, stderr
	args := os.Args[1:]
	cmd := exec.Command(claudeBin(), args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Pass through environment + ensure node is in PATH
	env := os.Environ()
	nodePath := nodeDir()
	if runtime.GOOS == "windows" {
		env = append(env, fmt.Sprintf("PATH=%s;%s", nodePath, os.Getenv("PATH")))
	} else {
		env = append(env, fmt.Sprintf("PATH=%s/bin:%s", nodePath, os.Getenv("PATH")))
	}
	cmd.Env = env

	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func setup() error {
	os.MkdirAll(dataDir(), 0755)

	// Step 1: Download Node.js if needed
	if _, err := os.Stat(nodeExe()); os.IsNotExist(err) {
		fmt.Println("[yoloclaude] Downloading Node.js", nodeVersion, "...")
		if err := downloadNode(); err != nil {
			return fmt.Errorf("failed to download Node.js: %w", err)
		}
		fmt.Println("[yoloclaude] Node.js installed.")
	}

	// Step 2: Install Claude Code
	fmt.Println("[yoloclaude] Installing Claude Code...")
	if err := installClaudeCode(); err != nil {
		return fmt.Errorf("failed to install Claude Code: %w", err)
	}
	fmt.Println("[yoloclaude] Claude Code installed. Launching...")
	fmt.Println()

	return nil
}

func downloadNode() error {
	var url string
	var arch string

	switch runtime.GOARCH {
	case "amd64":
		arch = "x64"
	case "arm64":
		arch = "arm64"
	default:
		arch = runtime.GOARCH
	}

	switch runtime.GOOS {
	case "windows":
		url = fmt.Sprintf("https://nodejs.org/dist/%s/node-%s-win-%s.zip", nodeVersion, nodeVersion, arch)
	case "darwin":
		url = fmt.Sprintf("https://nodejs.org/dist/%s/node-%s-darwin-%s.tar.gz", nodeVersion, nodeVersion, arch)
	case "linux":
		url = fmt.Sprintf("https://nodejs.org/dist/%s/node-%s-linux-%s.tar.gz", nodeVersion, nodeVersion, arch)
	default:
		return fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}

	tmpFile := filepath.Join(dataDir(), "node-download")
	if err := downloadFile(url, tmpFile); err != nil {
		return err
	}
	defer os.Remove(tmpFile)

	if runtime.GOOS == "windows" {
		return extractZipFlat(tmpFile, nodeDir())
	}
	return extractTarGzFlat(tmpFile, nodeDir())
}

func downloadFile(url, dest string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}

	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()

	written, err := io.Copy(f, resp.Body)
	if err != nil {
		return err
	}
	fmt.Printf("[yoloclaude] Downloaded %.1f MB\n", float64(written)/1024/1024)
	return nil
}

// extractZipFlat extracts a zip, stripping the top-level directory
func extractZipFlat(src, dest string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()

	os.MkdirAll(dest, 0755)

	for _, f := range r.File {
		// Strip top-level dir (e.g. "node-v22.15.0-win-x64/")
		parts := strings.SplitN(f.Name, "/", 2)
		if len(parts) < 2 || parts[1] == "" {
			continue
		}
		relPath := parts[1]
		fullPath := filepath.Join(dest, relPath)

		if f.FileInfo().IsDir() {
			os.MkdirAll(fullPath, 0755)
			continue
		}

		os.MkdirAll(filepath.Dir(fullPath), 0755)
		outFile, err := os.OpenFile(fullPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			outFile.Close()
			return err
		}

		io.Copy(outFile, rc)
		rc.Close()
		outFile.Close()
	}
	return nil
}

// extractTarGzFlat extracts a tar.gz, stripping the top-level directory
func extractTarGzFlat(src, dest string) error {
	cmd := exec.Command("tar", "xzf", src, "-C", dest, "--strip-components=1")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func installClaudeCode() error {
	dir := claudeDir()
	os.MkdirAll(dir, 0755)

	// Create package.json
	pkgJSON := filepath.Join(dir, "package.json")
	os.WriteFile(pkgJSON, []byte(`{"private":true}`), 0644)

	// Run npm install
	npmExe := filepath.Join(nodeDir(), "npm.cmd")
	if runtime.GOOS != "windows" {
		npmExe = filepath.Join(nodeDir(), "bin", "npm")
	}

	cmd := exec.Command(npmExe, "install", claudeCodePkg)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Ensure node is in PATH for npm
	env := os.Environ()
	if runtime.GOOS == "windows" {
		env = append(env, fmt.Sprintf("PATH=%s;%s", nodeDir(), os.Getenv("PATH")))
	} else {
		env = append(env, fmt.Sprintf("PATH=%s/bin:%s", nodeDir(), os.Getenv("PATH")))
	}
	cmd.Env = env

	return cmd.Run()
}
