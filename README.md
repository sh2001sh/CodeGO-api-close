# CodeGo V3

CodeGo is an AI API gateway with account management, credit billing, channel routing, and a marketplace.

The Go module lives in `v3/`; the React application lives in `web/app/`. The root V2 server, frontend, and release workflows have been removed. The independent V3 legacy importer remains available for database migration.

## Run locally

Requirements: Go matching `v3/go.mod`, Bun, Docker Compose, and Bash for backend verification.

Copy `.env.example` to a private `.env`, generate separate 32-byte base64 values for `V3_SECRET_KEY` and `V3_SESSION_SECRET`, and set an isolated `V3_TEST_PG_PASSWORD`.

```bash
make dev
docker compose --env-file .env -f v3/deploy/compose.test.yaml -p codego-v3-dev ps
```

The local application listens on `http://localhost:18083`. PostgreSQL, Redis, WAL, and uploaded files belong to this isolated Compose project. `make stop` preserves its volumes. Use `make dev-web` for the frontend development server.

## Verify

```bash
make verify
make verify-web
```

The backend checks use disposable databases. Deployment acceptance instructions, browser checks, configuration details, and migration commands are in [v3/deploy/README.md](v3/deploy/README.md). The architecture is described in [v3/README.md](v3/README.md).

## Build and release

```bash
docker build -f Dockerfile --build-arg SERVICE=gateway -t codego-gateway .
docker build -f v3/deploy/Dockerfile.web -t codego-web .
```

Backend `SERVICE` values are `gateway`, `control`, `worker`, and `migrate`. The V3 GitHub workflow verifies the backend, frontend, browsers, and isolated stack, then publishes five images for `main`, `v3-rebuild`, and pushed tags:

```text
ghcr.io/sh2001sh/codego-api:sha-<7-character-commit>-gateway-amd64
ghcr.io/sh2001sh/codego-api:sha-<7-character-commit>-control-amd64
ghcr.io/sh2001sh/codego-api:sha-<7-character-commit>-worker-amd64
ghcr.io/sh2001sh/codego-api:sha-<7-character-commit>-migrate-amd64
ghcr.io/sh2001sh/codego-api:sha-<7-character-commit>-web-amd64
```

`docker-compose.yml` is an image-only V3 template for a new installation with external PostgreSQL and Redis. Set `V3_IMAGE_TAG` to the verified immutable revision. Schema migration is an explicit operation:

```bash
docker compose --env-file .env --profile operations run --rm migrate schema
docker compose --env-file .env up -d worker gateway control web
```

For an existing installation, keep its deployment directory, environment, ports, volumes, network, and database configuration. The manual V3 deployment workflow updates image references in an existing JSON Compose file; it does not copy this template over production or run a database migration. It requires the exact full commit, a successful verification run for that commit, pinned SSH host keys, and the existing remote deployment path.

Runtime and capacity notes: [production operations](docs/production-performance-and-concurrency-guide.zh-CN.md).

## License

See [LICENSE](LICENSE), [NOTICE](NOTICE), and [THIRD-PARTY-LICENSES.md](THIRD-PARTY-LICENSES.md).
