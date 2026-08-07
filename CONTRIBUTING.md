# Contributing to Husonym

Thanks for wanting to contribute! This document covers how to get a local
environment running and what we expect in a pull request.

## Getting set up

Husonym is a Go monorepo (backend, worker, CLI) with a Next.js frontend and a
Docusaurus documentation site.

### Prerequisites

- Docker, with the `docker compose` subcommand
- Go (see the version in [`go.mod`](./go.mod))
- Node.js LTS and npm
- Optional: [aqua](https://aquaproj.github.io/) to install the pinned dev tools
  listed in [`aqua.yaml`](./aqua.yaml)

A [devcontainer](./.devcontainer/devcontainer.json) is available if you prefer a
preconfigured environment.

### Running the stack

The quickest way to get a working environment is the production compose file,
which pulls published images and pre-seeds connections and jobs:

```sh
make compose/up     # start
make compose/down   # stop
```

The app is then available at [http://localhost:3000](http://localhost:3000).

To work on the code itself, use the development compose file, which builds from
your local sources:

```sh
make compose/dev/up
make compose/dev/down
```

Run `make help` to see every available target.

### Building and linting

```sh
make build            # builds backend, worker and CLI
make lint             # lints Go and the frontend
go test ./...         # Go unit tests

cd frontend
npm install
npm run lint
npm run typecheck
npm run test
```

Documentation lives in [`docs/`](./docs/README.md) and has its own
`npm install && npm run start` loop.

## Pull requests

- **Branch from `main`** and open the PR against `main`.
- **Keep it focused.** One concern per pull request is much easier to review.
- **Use conventional commit prefixes** in the PR title, matching the existing
  history: `feat:`, `fix:`, `chore:`, `docs:`, `ci:`, `refactor:`, `test:`.
  A scope is welcome when it helps, e.g. `fix(worker): ...`.
- **Make CI pass.** Lint, tests and the docs build all run on pull requests.
- **Add tests** for behaviour changes, next to the code they cover.
- **Update the docs** when you change something user-facing.

### Generated code

Some code is generated and should not be edited by hand:

- Protobuf-derived Go, TypeScript and Python SDKs — regenerate with
  `make generate/backend` (see [`buf.gen.yaml`](./buf.gen.yaml))
- sqlc query code in the backend
- Helm chart `README.md` files — regenerate with `helm-docs`

If your change touches a `.proto` file or a `.sql` query, regenerate and commit
the result alongside your change.

## Reporting bugs and requesting features

Open a [GitHub issue](https://github.com/fishtre-compagnie/husonym/issues) using
the bug report or feature request template.

For anything security-related, please **do not** open a public issue — follow
[SECURITY.md](./SECURITY.md) instead.

## Code of conduct

Participation in this project is governed by our
[Code of Conduct](./CODE_OF_CONDUCT.md).
