# CodeGo V3 repository conventions

- Backend: the independent Go module `github.com/sh2001sh/new-api/v3` in `v3/`, PostgreSQL 15+, pgx, Redis, and `net/http`.
- Frontend: React, TypeScript, Rsbuild, Base UI, Tailwind CSS, and Lucide in `web/app/`. Use Bun and the committed lockfile.
- Entrypoints: `v3/cmd/gateway`, `control`, `worker`, and `migrate`.
- APIs: `v3/api/openapi.json`; regenerate backend contracts with `bash v3/scripts/generate.sh` and frontend types with `bun run generate:api` in `web/app/`.
- Backend verification: `bash v3/scripts/verify.sh boundaries test lint race integration pgtest atlas`. These database checks must use isolated test infrastructure.
- Frontend verification: `bun run typecheck`, `bun run check:api`, `bun run test`, and `bun run build` in `web/app/`.
- The V3 legacy importer reads old schemas for migration. It must not import the removed V2 application or become a runtime dependency on it.
- Production updates preserve existing secrets, database configuration, persistent WAL/files, and network settings. Pin all service images to one verified commit.
- Keep credentials, session artifacts, database files, build outputs, and operational snapshots out of version control.
