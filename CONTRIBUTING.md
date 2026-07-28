# 🎬 Contributing to Gridctl

Thank you for your interest in contributing to Gridctl! This guide will help you get started.

## 📒 Code of Conduct

Please read and follow our [Code of Conduct](CODE_OF_CONDUCT.md). We expect all contributors to be respectful and constructive.

## 🚦 Getting Started

### Prerequisites

- **Go** 1.26 or later (the version in `go.mod`)
- **Node.js** 20 or later
- **Docker** (for running containers and integration tests)
- **Git** with commit signing configured

### Development Setup

1. **Fork and clone the repository:**

   ```bash
   git clone https://github.com/YOUR_USERNAME/gridctl.git
   cd gridctl
   ```

2. **Install dependencies:**

   ```bash
   make deps
   ```

3. **Build the project:**

   ```bash
   make build
   ```

4. **Verify the build:**

   ```bash
   ./gridctl version
   ```

5. **Run tests:**

   ```bash
   make test
   ```

### Build Commands

| Command | Description |
|---------|-------------|
| `make build` | Build frontend and backend |
| `make build-web` | Build React frontend only |
| `make build-go` | Build Go binary only |
| `make dev` | Run Vite dev server for frontend development |
| `make test` | Run unit tests |
| `make test-coverage` | Run tests with coverage report |
| `make test-frontend` | Run frontend tests (Vitest) |
| `make test-integration` | Run integration tests (requires Docker) |
| `make generate` | Regenerate mocks under `pkg/mcp/` and `pkg/runtime/` |
| `make clean` | Remove build artifacts |

> The `gridctl` binary on your `$PATH` is typically a released build. Test local changes with `make build` followed by `./gridctl …`.

## 🔧 Making Changes

### Branch Naming

Create a branch with a descriptive name using one of these prefixes:

| Prefix | Use Case |
|--------|----------|
| `feature/` | New functionality |
| `fix/` | Bug fixes |
| `refactor/` | Code restructuring |
| `docs/` | Documentation changes |
| `chore/` | Maintenance, CI, dependencies |

Example: `feature/add-ssh-transport` or `fix/container-timeout`

### Code Conventions

**Go:**
- Use standard library when possible
- Return errors instead of panicking
- Use `log/slog` for logging
- Pass `context.Context` for cancellation
- Write table-driven tests
- Define interfaces for external dependencies to enable mocking

**TypeScript/React:**
- Functional components with hooks
- Zustand for state management
- Tailwind CSS for styling

### Testing Requirements

- New functionality, features, and behavior changes must include tests - not just new exported functions
- Use the `TestFunctionName_Scenario` naming pattern
- Table-driven tests are preferred for multiple test cases
- Maintain existing coverage levels

Run tests before submitting:

```bash
make test                  # Unit tests
make test-integration      # Integration tests (requires Docker)
```

## 📋 Commit Guidelines

### Commit Message Format

Use [conventional commits](https://www.conventionalcommits.org/):

```
<type>: <subject>
```

**Types:**
- `feat` - New feature
- `fix` - Bug fix
- `docs` - Documentation only
- `style` - Code style (formatting, semicolons, etc.)
- `refactor` - Code change that neither fixes a bug nor adds a feature
- `test` - Adding or correcting tests
- `chore` - Maintenance tasks
- `perf` - Performance improvement

**Rules:**
- Use imperative mood ("add feature" not "added feature")
- Keep subject under 50 characters
- No period at the end

**Examples:**
- `feat: add SSH transport support`
- `fix: resolve container timeout on slow networks`
- `docs: update stack configuration examples`

### Commit Signing

All commits must be signed. If you haven't set up GPG signing, see [GitHub's guide on signing commits](https://docs.github.com/en/authentication/managing-commit-signature-verification/signing-commits).

## 📜 Pull Request Process

1. **Create a feature branch** from `main`
2. **Make your changes** following the guidelines above
3. **Run tests** and ensure they pass
4. **Push your branch** to your fork
5. **Open a pull request** against the `main` branch

### PR Requirements

Your pull request should:
- Have a clear description of the changes
- Reference any related issues
- Pass all CI checks (linting, tests, build)
- Add an entry under `[Unreleased]` in [CHANGELOG.md](CHANGELOG.md) for any user-visible change
- Follow the [PR template](.github/pull_request_template.md) checklist

[CONSTITUTION.md](CONSTITUTION.md) is binding for every change and covers the rules that most often catch a refactor by surprise: test-first development, real dependencies in integration tests, no panics in `pkg/` or `internal/`, context propagation, stack YAML back-compat, and changelog discipline.

### CI Checks

Pull requests are automatically checked for:
- Go linting (`golangci-lint` with `gosec`)
- Vulnerability scanning (`govulncheck`, `npm audit`)
- Unit tests with race detection, plus overall and per-package coverage thresholds
- Integration tests against both Docker and Podman
- Frontend type check, lint, tests, and build
- Example stack validation
- Successful binary build

## 🚧 Issue Guidelines

### Bug Reports

Use the [bug report template](.github/ISSUE_TEMPLATE/1-bug-report.yml) and include:
- Clear description of the issue
- Steps to reproduce
- Expected vs actual behavior
- Environment details (OS, version, etc.)

### Feature Requests

Use the [feature request template](.github/ISSUE_TEMPLATE/2-feature-request.yml) and include:
- Problem statement
- Proposed solution
- Alternatives considered

Before opening an issue, please search existing issues to avoid duplicates.

## 🪾 Project Structure

```
gridctl/
├── cmd/gridctl/           # CLI entry point
├── internal/              # Internal packages
├── pkg/                   # Public packages
│   ├── config/            # Stack YAML parsing
│   ├── runtime/           # Container orchestration
│   ├── mcp/               # MCP protocol implementation
│   ├── registry/          # Skills registry (agentskills.io spec)
│   └── skills/            # Remote skill management (import, update)
├── web/                   # React frontend
├── examples/              # Example stacks
└── tests/integration/     # Integration tests
```

[AGENTS.md](AGENTS.md) carries the full package-by-package map and the end-to-end request flow.

## 🪪 License

By contributing to Gridctl, you agree that your contributions will be licensed under the [Apache License 2.0](LICENSE).
