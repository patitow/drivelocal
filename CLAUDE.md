# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project overview

DriveLocal is a LAN media server: a Go backend (`server-go/`) serving a React + TypeScript + Vite frontend (`client/`), packaged for Docker. It was migrated from an older Node/Express server (the vestiges of which still litter the repo root — see gotchas below).

## Commands

**Go backend** (`server-go/`, Go 1.24, module `drivelocal`):
```
cd server-go && go run .                     # dev server
cd server-go && go build -o drivelocal-server . # build
cd server-go && go test ./...                 # run tests
```
No Makefile or wrapper script exists for these — run them directly from `server-go/`. There is no `.golangci.yml`; linting is whatever `go vet`/`gofmt` you run manually.

**Client** (`client/`, React 18 + TS + Vite):
```
cd client && npm run dev          # vite dev server
cd client && npm run build        # tsc && vite build
cd client && npm run type-check   # tsc --noEmit
cd client && npx vitest           # unit tests (no npm script wired to this)
cd client && npm run lint         # eslint, --max-warnings 0 (flat config: client/eslint.config.js)
cd client && npm run format       # prettier --write (config: client/.prettierrc.json)
cd client && npm run format:check
```
ESLint/Prettier configs were added by `/init` (2026-08-19) — before that, `eslint`/`prettier` were installed but unconfigured and `npm run lint` errored outright. The lint config now runs cleanly but surfaces real pre-existing issues in the codebase (mostly `@typescript-eslint/no-explicit-any` and a few Tailwind/a11y warnings); since the script uses `--max-warnings 0`, `npm run lint` currently exits non-zero until those are cleaned up. `husky`/`lint-staged` are still devDependencies with no `.husky/` directory — pre-commit hooks are not wired up.

**E2E** (Playwright, config at repo root, specs in `tests/*.spec.js`):
```
npm run test:playwright       # baseURL http://localhost:3000
```
By default this auto-starts `npm run dev` as a webServer; set `DEV_MODE=true` if a dev server is already running and you don't want Playwright to start its own.

**Root `package.json` gotcha**: `npm test` (jest) and `npm run lint` (eslint) target a `server/` directory that no longer exists (pre-Go-migration leftover) — they will find nothing or error. Don't use them; use the Go/client commands above instead. Likewise the many `scripts/test-*.js` files and their `test:*` npm scripts are leftovers from the old Node/Express server era — treat as unreliable against the current Go backend.

## Dev ports

Dev mode: Vite frontend on 3000, API on 5000 (no admin/root needed). Production/Docker: port 80 (needs elevation on Windows, hence Docker). `docker-compose.yml` also maps `127.0.0.1:8080:80` if host port 80 is already in use.

## Git conventions

Everything is committed directly to `main` — no feature-branch or PR workflow in use. Commit messages are plain sentence-case summaries, no conventional-commits prefix.

## Critical gotcha: local credentials

`server-go/server/data/users.json` is gitignored. Copy `users.example.json` to `users.json` for local/dev. Do not commit real emails or password hashes.

## Docker media mounts

Host media paths belong in `docker-compose.override.yml` (gitignored). Use `docker-compose.override.example.yml` as a template. The committed `docker-compose.yml` does not bind-mount host media; a read-only `/data` parent would block nested mounts.
