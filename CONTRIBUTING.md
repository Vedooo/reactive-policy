# Contributing to reactive-policy

Thanks for your interest. Contributions are welcome — especially new action plugins.

## Quick links

- [Architecture overview](docs/ARCHITECTURE.md)
- [CRD specification](docs/CRD_SPEC.md)
- [Plugin development guide](docs/PLUGIN_INTERFACE.md)
- [Roadmap](docs/ROADMAP.md)
- [Design decisions](docs/DECISIONS.md)

## Development setup

You can develop on any Linux machine (a VM works well too) that has the
prerequisites below.

Prerequisites:

- Go 1.22+
- Docker or compatible container runtime
- `kubectl`, `kind`, `helm`, `kubebuilder` v4
- `golangci-lint` 1.61+

```bash
git clone https://github.com/Vedooo/reactive-policy.git
cd reactive-policy
make deps
make test
make kind-up
make install
make run
```

## Common tasks

### Run tests

```bash
make test
make test-coverage
make e2e
```

### Add a new action plugin

See [docs/PLUGIN_INTERFACE.md](docs/PLUGIN_INTERFACE.md). Summary:

1. Create `plugins/your-plugin/plugin.go`
2. Implement the `action.Action` interface
3. Register via `init()` calling `action.Register(...)`
4. Add tests (90%+ coverage)
5. Add an underscore import to `cmd/main.go` (operator) and `cmd/cli/main.go` (CLI)
6. Update the plugin table in `docs/PLUGIN_INTERFACE.md` section 5
7. Add a sample policy to `config/samples/`

### Build the binaries

```bash
make build       # operator -> bin/manager
make build-cli   # rp CLI   -> bin/rp
```

### Submit a pull request

1. Fork and create a feature branch from `main`
2. Ensure `make lint test` passes
3. Update `CHANGELOG.md` under "Unreleased"
4. Open a PR with a clear Conventional Commits title

## Code review process

- I review pull requests within 5 working days
- Small PRs get faster reviews
- CI must be green before review

## Conduct

Follow the project's [Code of Conduct](CODE_OF_CONDUCT.md).

## Reporting security issues

See [SECURITY.md](SECURITY.md). Use private vulnerability reporting; do not file
public issues for security problems.

## License

By contributing, you agree your contributions will be licensed under Apache
License 2.0.
