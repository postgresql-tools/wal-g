# Contributing to WAL-G

Thanks for your interest in contributing!

## Development Setup

```bash
git clone https://github.com/lateos-ai/wal-g.git
cd wal-g
go mod download
go test ./...
```

## Making Changes

1. Create a branch: `git checkout -b feature/your-feature`
2. Make changes
3. Run tests: `go test ./...`
4. Commit: `git commit -am "feat: description"`
5. Push: `git push origin feature/your-feature`
6. Create Pull Request

All PRs must:
- Pass all tests
- Have clear commit messages
- Update documentation

## Reporting Issues

Use GitHub Issues. Include:
- Steps to reproduce
- Expected vs actual behavior
- Your environment (OS, Go version, PostgreSQL version)
