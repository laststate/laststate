# Contributing to LastState Trace

Thank you for your interest in contributing to the LastState Trace platform!

## Development Setup

### Prerequisites

- Go 1.25+
- PostgreSQL 16+
- Redis (for caching)
- NATS (for messaging)

### Building

```bash
# Install dependencies
go mod download

# Build the binary
go build -o bin/trace ./cmd/laststate

# Run tests
go test ./...

# Run with race detection
go test -race ./...
```

### Running Locally

```bash
# Start dependencies
docker-compose up -d

# Run migrations
go run cmd/laststate/main.go --migrate

# Start the server
go run cmd/laststate/main.go
```

## Contributing Guidelines

### Code Style

- Follow [Effective Go](https://go.dev/doc/effective_go)
- Use `gofmt` and `goimports`
- Write tests for all new functionality
- Document public APIs with GoDoc comments

### Commit Messages

- Use [Conventional Commits](https://www.conventionalcommits.org/)
- Format: `type(scope): description`
- Types: feat, fix, docs, style, refactor, test, chore

### Pull Requests

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests
5. Submit a PR with a clear description

### Issue Reporting

- Use the [bug report template](.github/ISSUE_TEMPLATE/bug.yml)
- Use the [feature request template](.github/ISSUE_TEMPLATE/feature.yml)
- Include steps to reproduce for bugs
- Include use case for features

## Code of Conduct

See [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md)

## License

See [LICENSE](LICENSE)
