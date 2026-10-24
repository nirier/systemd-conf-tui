# Project guidance

- This is a Go / Bubble Tea TUI for creating systemd service units on Linux.
- Keep the default flow short and Chinese-language: basics, runtime, preview.
- User services are the default. System mode must elevate only installation
  commands through sudo, with the terminal released for authentication.
- Keep unit rendering and validation in `internal/unit`, filesystem/systemctl
  operations in `internal/service`, and interaction in `internal/tui`.
- Never overwrite existing target files or run user input through a shell.
- Preserve a complete preview before installation and report partial success
  accurately. Do not start or install actual services during automated tests.
- Run `gofmt`, `go test ./...`, `go vet ./...`, and `go build ./...` after changes.
