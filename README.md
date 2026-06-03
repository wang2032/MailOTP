# MailOTP

Catch-all email OTP inbox MVP.

This repository contains three deployable parts:

- `apps/api`: Go backend with PostgreSQL storage.
- `apps/web`: Next.js frontend for creating inbox aliases and viewing the latest OTP.
- `apps/worker`: Cloudflare Email Worker that receives routed mail and posts parsed messages to the API.

The MVP flow is:

1. Cloudflare Email Routing receives `*@your-domain.com`.
2. The Email Worker extracts the local alias and the likely OTP code.
3. The Worker calls `POST /mail` on the API with a bearer token.
4. The API stores the message in PostgreSQL.
5. The Next.js frontend reads inbox state from the API.

This project is intended for inboxes and domains you own and operate. It does not include automation for creating third-party accounts or bypassing platform rules.

## Local Development

Start PostgreSQL:

```bash
docker compose up -d db
```

Start the API:

```bash
cd apps/api
cp .env.example .env
go run ./cmd/api
```

Start the web app:

```bash
cd apps/web
cp .env.example .env.local
npm install
npm run dev
```

Open `http://localhost:3000`.

## Docker Image

The root `Dockerfile` builds one container that serves both the Go API and the Vite frontend.

Build locally:

```bash
docker build -t mailotp:latest .
```

Run locally:

```bash
docker run --rm -p 18080:8080 \
  -e DATABASE_URL='postgres://postgres:password@host.docker.internal:35432/mailotp?sslmode=disable' \
  -e REDIS_URL='redis://host.docker.internal:26739' \
  -e WEBHOOK_SECRET='change-me' \
  -e MAIL_DOMAIN='joeystory.xyz' \
  --add-host host.docker.internal:host-gateway \
  mailotp:latest
```

For DockerHub automated builds, use the repository root as the build context and `Dockerfile` as the Dockerfile path.

## GitHub Actions to DockerHub and Server

The workflow at `.github/workflows/develop.yml` builds the image, pushes it to DockerHub, then deploys it on the server with Docker Compose.

Configure these GitHub repository secrets:

- `DOCKERHUB_USERNAME`: DockerHub username, for example `joeywang2032`
- `DOCKERHUB_TOKEN`: DockerHub access token
- `DEPLOY_HOST`: deployment server host, for example `117.72.157.82`
- `DEPLOY_USER`: SSH user, for example `root`
- `DEPLOY_PASSWORD`: SSH password

Runtime app configuration lives on the server in `/opt/mailotp/.env`, not in GitHub Secrets:

```bash
DOCKER_IMAGE=joeywang2032/mailotp:latest
DATABASE_URL=postgres://postgres:<password>@host.docker.internal:35432/mailotp?sslmode=disable
REDIS_URL=redis://host.docker.internal:26739
WEBHOOK_SECRET=<shared-secret>
MAIL_DOMAIN=joeystory.xyz
CORS_ORIGINS=http://joeystory.xyz,http://117.72.157.82:18080
```

The deploy job writes `/opt/mailotp/docker-compose.yml` on the server and runs:

```bash
docker compose --env-file .env pull
docker compose --env-file .env up -d
```

## Cloudflare

1. Put your domain on Cloudflare DNS.
2. Enable Email Routing.
3. Create a catch-all route to the Worker in `apps/worker`.
4. Set Worker secrets:

```bash
cd apps/worker
npm install
npx wrangler secret put API_URL
npx wrangler secret put WEBHOOK_SECRET
npx wrangler secret put MAIL_DOMAIN
npm run deploy
```

Use the same `WEBHOOK_SECRET` in `apps/api/.env`.

## API

- `POST /api/inboxes`: create an inbox alias.
- `GET /api/inboxes/{alias}`: get inbox and recent messages.
- `GET /api/inbox/{alias}`: get the latest OTP in the simple MVP response shape.
- `POST /mail`: Worker webhook, requires `Authorization: Bearer <WEBHOOK_SECRET>`.

## Database

The API can create tables on startup when `AUTO_CREATE_TABLES=true`.
For managed PostgreSQL, the schema is also available at `apps/api/sql/schema.sql`.
