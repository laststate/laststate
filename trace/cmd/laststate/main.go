// Package main provides the `laststate` CLI for self-hosted deployment.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
)

const (
	version = "0.10.0"
	appName = "laststate"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(0)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "init":
		cmdInit(args)
	case "up":
		cmdUp(args)
	case "down":
		cmdDown(args)
	case "status":
		cmdStatus(args)
	case "logs":
		cmdLogs(args)
	case "config":
		cmdConfig(args)
	case "bootstrap":
		cmdBootstrap(args)
	case "version":
		fmt.Printf("%s version %s\n", appName, version)
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Printf(`LastState v%s — self-hosted embedded observability

Usage:
  laststate <command> [options]

Commands:
  init        Initialize a new LastState deployment directory
  up          Start all services (Trace, Relay, PostgreSQL)
  down        Stop all services
  status      Show deployment status
  logs        Follow service logs
  config      Show current configuration
  bootstrap   Generate bootstrap tokens
  version     Show version

Run 'laststate <command> --help' for command-specific help.
`, appName)
}

func projectDir() string {
	if cwd, err := os.Getwd(); err == nil {
		return cwd
	}
	return "."
}

func dataDir() string {
	return filepath.Join(projectDir(), "laststate-data")
}

func composeFile() string {
	return filepath.Join(projectDir(), "docker-compose.laststate.yml")
}

func cmdInit(args []string) {
	dd := dataDir()

	if _, err := os.Stat(dd); err == nil {
		fmt.Printf("LastState data directory already exists at %s\n", dd)
		return
	}

	if err := os.MkdirAll(dd, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating data directory: %v\n", err)
		os.Exit(1)
	}

	// Generate bootstrap token
	token := generateToken()

	// Write bootstrap files
	os.WriteFile(filepath.Join(dd, "bootstrap-token.txt"), []byte(token), 0600)
	os.WriteFile(filepath.Join(dd, "bootstrap-admin.txt"), []byte("admin@localhost\nchangeme"), 0600)

	// Write docker-compose
	compose := generateCompose()
	os.WriteFile(composeFile(), []byte(compose), 0644)

	fmt.Println("✅ LastState initialized!")
	fmt.Printf("   Data directory: %s\n", dd)
	fmt.Printf("   Ingest token:   %s\n", token)
	fmt.Printf("   Admin:          admin@localhost / changeme\n")
	fmt.Printf("   Compose file:   %s\n", composeFile())
	fmt.Println("\n   Next: laststate up")
}

// generateCompose returns a docker-compose.yml for the Trace stack.
func generateCompose() string {
	return `version: "3.8"

services:
  trace:
    build: ..
    ports:
      - "8080:8080"
    environment:
      - TRACE_DATABASE_URL=postgres://trace:trace@postgres:5432/trace?sslmode=disable
      - TRACE_OBJECT_DIR=/data/objects
      - TRACE_QUEUE=postgres
      - TRACE_MOCK=true
      - TRACE_OPEN_UI=true
    volumes:
      - trace-data:/data
    depends_on:
      postgres:
        condition: service_healthy

  postgres:
    image: postgres:16-alpine
    ports:
      - "5432:5432"
    environment:
      - POSTGRES_USER=trace
      - POSTGRES_PASSWORD=trace
      - POSTGRES_DB=trace
    volumes:
      - postgres-data:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U trace"]
      interval: 5s
      timeout: 5s
      retries: 5

volumes:
  trace-data:
  postgres-data:
`
}

func cmdUp(args []string) {
	dd := dataDir()
	cf := composeFile()

	if _, err := os.Stat(dd); os.IsNotExist(err) {
		fmt.Println("No deployment found. Run 'laststate init' first.")
		os.Exit(1)
	}

	if _, err := os.Stat(cf); os.IsNotExist(err) {
		fmt.Println("No compose file found. Run 'laststate init' first.")
		os.Exit(1)
	}

	fmt.Println("🚀 Starting LastState...")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\n🛑 Shutting down...")
		cancel()
	}()

	cmd := exec.CommandContext(ctx, "docker", "compose", "-f", cf, "up", "-d")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error starting services: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("✅ All services started!")
	fmt.Println("\n   Trace UI:    http://localhost:8080")
	fmt.Println("   Relay:       http://localhost:8081")
	fmt.Println("   PostgreSQL:  localhost:5432")
	fmt.Println("\n   Dashboard:   http://localhost:8080")
	fmt.Println("   Metrics:     http://localhost:8080/metrics")
}

func cmdDown(args []string) {
	cf := composeFile()
	if _, err := os.Stat(cf); os.IsNotExist(err) {
		fmt.Println("No deployment found.")
		os.Exit(1)
	}

	fmt.Println("🛑 Stopping LastState...")
	cmd := exec.Command("docker", "compose", "-f", cf, "down")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error stopping services: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✅ All services stopped.")
}

func cmdStatus(args []string) {
	cf := composeFile()
	if _, err := os.Stat(cf); os.IsNotExist(err) {
		fmt.Println("No deployment found.")
		os.Exit(1)
	}

	cmd := exec.Command("docker", "compose", "-f", cf, "ps", "--format", "json")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Run()

	// Show summary
	cmd2 := exec.Command("docker", "compose", "-f", cf, "ps")
	cmd2.Stdout = os.Stdout
	cmd2.Stderr = os.Stderr
	cmd2.Run()
}

func cmdLogs(args []string) {
	cf := composeFile()
	svc := "trace"
	if len(args) > 0 {
		svc = args[0]
	}

	cmd := exec.Command("docker", "compose", "-f", cf, "logs", "-f", svc)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func cmdConfig(args []string) {
	dd := dataDir()
	cf := composeFile()

	fmt.Println("LastState Configuration")
	fmt.Println("=======================")
	fmt.Printf("Project directory: %s\n", projectDir())
	fmt.Printf("Data directory:    %s\n", dd)
	fmt.Printf("Compose file:      %s\n", cf)

	// Show bootstrap tokens
	tokenFile := filepath.Join(dd, "bootstrap-token.txt")
	if data, err := os.ReadFile(tokenFile); err == nil {
		fmt.Printf("\nIngest token: %s\n", strings.TrimSpace(string(data)))
	}

	adminFile := filepath.Join(dd, "bootstrap-admin.txt")
	if data, err := os.ReadFile(adminFile); err == nil {
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")
		if len(lines) >= 2 {
			fmt.Printf("Admin email:   %s\n", lines[0])
			fmt.Printf("Admin password: %s\n", lines[1])
		}
	}
}

func cmdBootstrap(args []string) {
	dd := dataDir()
	if _, err := os.Stat(dd); os.IsNotExist(err) {
		fmt.Println("No deployment found. Run 'laststate init' first.")
		os.Exit(1)
	}

	token := generateToken()
	os.WriteFile(filepath.Join(dd, "bootstrap-token.txt"), []byte(token), 0600)
	fmt.Printf("✅ New ingest token generated: %s\n", token)
	fmt.Println("   Restart services: laststate down && laststate up")
}

func generateToken() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return "ls_" + hex.EncodeToString(b)
}
