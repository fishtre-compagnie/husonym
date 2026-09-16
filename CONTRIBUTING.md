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

## Releasing

Pushing a `v*.*.*` tag on `main` runs `Artifact Release`, which publishes the container
images, the Helm charts and the CLI binaries.

```console
git tag -a v0.2.0 -m 'Husonym v0.2.0'
git push origin v0.2.0
```

The tag is what produces the `latest` image tag: `metadata-action` derives it from
`type=semver`, so a push to `main` alone publishes `main` and `sha-…` tags but never
`latest`. Since `compose.yml`, the README and the deploy docs all point at `:latest`,
**a release is what makes those instructions true** — this is not a cosmetic step.

Tagging is therefore outward-facing. It moves `latest`, which is the reference customers
follow.

### Release signing

The CLI release is signed: GoReleaser signs the `husonym_<version>_SHA256SUMS` file, so a
user can verify a download came from us. See
[Installing the CLI](./docs/docs/cli/installing.md) for the verification steps we ask of
them.

Two halves have to stay in sync, and this is the part that is easy to get wrong:

- **Infisical** (`prod`) holds `GPG_PRIVATE_KEY`, `GPG_PRIVATE_KEY_PASSPHRASE` and
  `GPG_REVOCATION_CERT` — the source of truth and the only off-machine copy.
- **GitHub Actions secrets** hold `GPG_PRIVATE_KEY` and `GPG_PRIVATE_KEY_PASSPHRASE`,
  because the workflow reads `${{ secrets.* }}` and cannot reach Infisical. Storing the
  key only in Infisical does not work.

Rotating means replacing both, and committing the new public key to
[`cli/release-signing-key.asc`](./cli/release-signing-key.asc) — whose fingerprint is
quoted in the install docs and must be updated there too. Signatures already made stay
verifiable with the old public key, so old releases do not break.

The key carries no expiry date, deliberately: an expired key would fail a release with no
warning, months after anyone last thought about it. Revocation is the escape hatch instead,
which is why the revocation certificate is kept in Infisical rather than generated on the
day it is needed.

## Reporting bugs and requesting features

Open a [GitHub issue](https://github.com/fishtre-compagnie/husonym/issues) using
the bug report or feature request template.

For anything security-related, please **do not** open a public issue — follow
[SECURITY.md](./SECURITY.md) instead.

## Code of conduct

Participation in this project is governed by our
[Code of Conduct](./CODE_OF_CONDUCT.md).
