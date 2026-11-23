# Local Marketplace — development starter

Starter project for a local-products marketplace:
- Go + Gin API
- PostgreSQL
- Redis
- React + TypeScript + Vite
- Docker Compose

## Requirements
- Docker Desktop (or Docker Engine + Compose plugin)
- Git

## Run the whole development environment

```bash
docker compose up --build
```

Open:
- Frontend: http://localhost:5173
- API health: http://localhost:8080/api/v1/health
- PostgreSQL: localhost:5432
- Redis: localhost:6379

Stop containers:

```bash
docker compose down
```

Stop and delete local database/cache data (destructive):

```bash
docker compose down -v
```

## Run backend locally (optional)

Start only infrastructure:

```bash
docker compose up -d postgres redis
```

Then:

```bash
cd backend
go mod tidy
go run ./cmd/api
```

When running Go on your Mac, use `DATABASE_URL` with `localhost:5432` and `REDIS_ADDR=localhost:6379`.

## Project structure

```text
backend/
  cmd/api/          application entry point
  internal/config/  environment configuration
  internal/platform/ database and Redis connections
  internal/transport/http/ HTTP routes and handlers
frontend/
  src/              React application
docker-compose.yml  local development services
```

## Notes
- This is a development foundation, not a production deployment.
- Change all development passwords before deploying.
- The API includes health checks and authentication endpoints for registration, login, token
  refresh, logout and the current user. New accounts always receive the `user` role.
- Auth tables are created idempotently when the API starts. Promote an account to admin by running
  `UPDATE users SET role = 'admin' WHERE email = 'admin@example.com';` in PostgreSQL, then sign in
  again to receive a JWT with the updated role.
- Set a unique random `JWT_SECRET` of at least 32 characters in `.env` before deploying.
- Uploaded product images are limited to 5 MB and stored in the persistent `product_images`
  Docker volume. `docker compose down -v` also deletes uploaded images.

## Authentication API

- `POST /api/v1/auth/register` with `{ "name": "Mai", "email": "mai@example.com", "password": "at-least-8-chars" }`
- `POST /api/v1/auth/login` with `{ "email": "mai@example.com", "password": "..." }`
- `POST /api/v1/auth/refresh` with `{ "refresh_token": "..." }` rotates the one-time refresh token.
- `POST /api/v1/auth/logout` with `{ "refresh_token": "..." }` revokes it.
- `GET /api/v1/auth/me` requires `Authorization: Bearer <access_token>`.
- `GET /api/v1/admin/ping` requires an admin access token.

Successful register/login/refresh responses include `access_token`, `refresh_token`,
`token_type`, `expires_in` and the user profile. Access tokens last 15 minutes; refresh tokens
last 30 days and are stored as hashes in PostgreSQL.

## Catalog API

Public routes return active products and all categories:

- `GET /api/v1/categories` and `GET /api/v1/categories/:id`
- `GET /api/v1/products` and `GET /api/v1/products/:id`
- Product list query parameters: `q`, `category_id`, `min_price`, `max_price`, `page`, `limit`,
  `sort_by` (`name`, `price`, `created_at`) and `sort_order` (`asc`, `desc`). The default page size
  is 12 and the maximum is 100. Responses include `data`, `page`, `limit` and `total`.

Admin routes require an admin Bearer access token:

- `GET/POST /api/v1/admin/categories`; `PUT/DELETE /api/v1/admin/categories/:id`
- `GET/POST /api/v1/admin/products`; `PUT/DELETE /api/v1/admin/products/:id`
- `POST /api/v1/admin/uploads` with multipart form field `image`; accepted types are JPEG, PNG and GIF.

The frontend lists/searches/filters/paginates active products. Admin users see the management
panel in the top navigation, where they can create, edit and delete categories and products, upload
images, and hide or publish products. Catalog tables are created idempotently when the API starts.
