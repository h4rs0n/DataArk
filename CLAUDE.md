# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

DataArk is a web-based data preservation and search system that stores and indexes HTML web pages. It consists of a Go backend API and Vue 3 frontend, using PostgreSQL for user data and Meilisearch for full-text search.

## Architecture

### Monorepo Structure
- `api/` - Go backend service (Gin framework + GORM)
- `web/` - Vue 3 frontend (TypeScript + Vite)
- `docker/` - Docker deployment configuration

### Backend Architecture (`api/`)
- `api/api/` - HTTP controllers and JWT authentication middleware
- `api/common/` - Shared utilities (database, auth, HTML processing, config)
- `api/search/` - Meilisearch integration for document indexing/search
- `api/assets/` - Embedded web assets (served by Go binary)

Key patterns:
- JWT-based authentication with 7-day token expiration
- Flag-based configuration (no config files - all CLI flags)
- Embedded frontend assets via Go embed FS
- Files stored in domain-based directories under archive location
- Go module uses relative import paths: `DataArk/common`, `DataArk/search`, `DataArk/assets`

### Frontend Architecture (`web/`)
- Vue 3 with Composition API and TypeScript
- Arco Design component library
- Pinia for state management
- Vue Router with hash-based routing
- Authentication guard in router beforeEach hook

Routes: `/`, `/search`, `/login`, `/upload`, `/htmlviewer`

## Development Commands

### Build Commands (using makefile)
```bash
make all        # Build everything: web -> web2api -> api
make web        # Build frontend only (cd web && npm i && npm run build)
make api        # Build backend only (go build -> bin/EchoArkServer)
make web2api    # Move built web assets to api/assets/web/
make clean      # Remove build artifacts (bin/, web/dist/)
```

Note: The README references `make build` but the actual target is `make api`.

### Frontend Development
```bash
cd web
npm i          # Install dependencies
npm run dev    # Start dev server (Vite)
npm run build  # Production build
```

### Backend Development
```bash
cd api
go mod tidy    # Sync dependencies
go run main.go # Run with flags (see Configuration section)
```

### Testing
No test suite currently exists in the project.

### Docker Deployment
```bash
cd docker
sudo docker compose build
sudo docker compose up -d
```

First run generates random admin password - check with `sudo docker compose logs`.

Docker uses multi-stage builds: node:20 for frontend -> golang:1.23 for backend -> alpine:3 for runtime.

## Configuration

The Go backend uses CLI flags for all configuration (see `api/common/flag.go`):

Required flags:
- `-loc` - Archive file storage location (default: `./api/static/archive/`)
- `-mhost` - Meilisearch host (default: `http://127.0.0.1:7700`)
- `-mkey` - Meilisearch API key (required for JWT signing)
- `-dbhost` - PostgreSQL host
- `-dbport` - PostgreSQL port (default: `5432`)
- `-dbname` - Database name (default: `echoark`)
- `-dbuser` - Database user (default: `postgres`)
- `-dbpasswd` - Database password (default: `postgres`)
- `-debug` - Enable debug mode (adds CORS headers)

Example:
```bash
./bin/EchoArkServer -loc ./archive -mhost "http://meili:7700" -mkey "masterkey" -dbhost "database"
```

The server listens on `0.0.0.0:7845` by default.

## Key Dependencies

### Backend
- `github.com/gin-gonic/gin` - Web framework
- `gorm.io/gorm` + `gorm.io/driver/postgres` - ORM and PostgreSQL driver
- `github.com/meilisearch/meilisearch-go` - Search client
- `github.com/golang-jwt/jwt/v5` - JWT authentication
- `github.com/google/uuid` - UUID generation for document IDs

### Frontend
- `vue` (v3.4+) - UI framework
- `@arco-design/web-vue` - Component library
- `pinia` - State management
- `vue-router` - Routing
- `axios` - HTTP client

## Database

PostgreSQL database with single `users` table:
- `id` (uint, primary key)
- `username` (string, unique, not null)
- `password` (string, bcrypt hashed, not null)
- `created_at`, `updated_at` (time)

Auto-migration runs on startup. Default admin user created on first run with a random password.

## Search

Meilisearch index named `blogs` with documents containing:
- `id` (UUID, primary key)
- `title` (extracted from HTML <title>)
- `filename` (original filename)
- `domain` (origin domain string)
- `content` (plain text extracted from HTML)

Files are stored under `{archive_location}/{domain}/{filename}` after indexing.

## API Endpoints

Public:
- `POST /api/login` - User login (returns JWT token)

Protected (require JWT auth):
- `GET /api/search?q=keyword&p=page` - Search documents
- `POST /api/uploadHtmlFile` - Upload HTML file (multipart form)
- `POST /api/upload` - Add document by filename + domain
- `GET /api/authChecker` - Verify token validity
- `POST /api/register` - Create new user
- `GET /archive/*` - Serve archived HTML files

## Frontend Auth Flow

1. Login -> Store JWT token in `localStorage`
2. Router guard checks token on every navigation
3. API calls include `Authorization: Bearer {token}` header
4. 401 responses clear token and redirect to `/login`