# Contributing to Conga Line

Thanks for your interest in contributing! This guide covers how to get involved.

## Reporting Bugs and Requesting Features

Open a [GitHub Issue](https://github.com/cruxdigital-llc/conga-line/issues). Include steps to reproduce for bugs.

## Development Setup

1. Clone the repo
2. See the [Development section](README.md#development) in the README for build instructions

## Pull Request Process

1. Fork the repository
2. Create a feature branch from `main`
3. Make your changes
4. Run tests and linting (see below)
5. Open a PR against `main`

## Code Style

- **Go**: `gofmt` (enforced)
- **Terraform**: `terraform fmt`
- **Shell scripts**: checked with `shellcheck`

## Testing

```bash
cd cli && go test ./...
cd terraform && terraform validate
```

## Code of Conduct

This project follows the [Contributor Covenant v2.1](CODE_OF_CONDUCT.md).
