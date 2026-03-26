# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

YoloClaude — a self-contained CLI (Go) that talks to the Anthropic Messages API with streaming support. Builds to native binaries (including .exe) via GitHub Actions.

## Build & Run

```bash
go build -o yoloclaude .          # build locally
go run . "your prompt here"       # one-shot mode
go run .                          # interactive REPL mode
ANTHROPIC_API_KEY=sk-... ./yoloclaude
```

## Environment Variables

- `ANTHROPIC_API_KEY` (required) — API key
- `YOLO_MODEL` — model to use (default: `claude-sonnet-4-6`)
- `YOLO_SYSTEM_PROMPT` — custom system prompt

## Release

Push a `v*` tag to trigger the GitHub Actions workflow, which cross-compiles for windows/amd64, darwin/amd64, darwin/arm64, linux/amd64 and creates a GitHub Release with all binaries.

## Architecture

Single-file Go app (`main.go`). Two modes:
- **One-shot**: args joined as prompt, non-streaming response, exits.
- **Interactive**: REPL with conversation history, streaming SSE response. `/clear` resets history.

API calls go through `sendMessage` (non-streaming) and `streamMessage` (SSE streaming). Version is injected at build time via `-ldflags`.
