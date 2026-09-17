# Daymark

Daymark is a small, self-hosted calendar for tracking recurring events and seeing their patterns over time. Define markers such as “Morning run”, “Headache”, or “Read”, then select a day to record any combination of them.

## Features

- Define, rename, recolor, and delete event types.
- Mark each event once per day, with any number of different events on the same day.
- Navigate a monthly calendar that works on desktop and mobile browsers.
- See weekly and monthly counts, averages, streaks, and all-time totals for each event.
- Keep data in a local SQLite database with no external service or account required.
- Run as a single container on `linux/amd64` and `linux/arm64`.

## Run with Docker Compose

```sh
docker compose up -d --build
```

Open [http://localhost:8080](http://localhost:8080). Calendar data is stored in the `daymark-data` Docker volume and survives container replacement.

To stop the app:

```sh
docker compose down
```

`docker compose down -v` also removes the database volume and all tracked data.

## Run locally

Go 1.24 or newer is required.

```sh
go run .
```

The service listens on port `8080` and stores its database at `./data/calendar.db`. Override these locations with `PORT` and `DATA_DIR`:

```sh
PORT=3000 DATA_DIR=/var/lib/daymark go run .
```

## Build multi-architecture images

With a Docker Buildx builder configured:

```sh
docker buildx build \
  --platform linux/amd64,linux/arm64 \
  --tag your-registry/daymark:latest \
  --push .
```

## Publish from GitHub Actions

The workflow in `.github/workflows/docker.yml` builds the image for AMD64 and ARM64. Pull requests are built for validation without being pushed. Pushes to the default `main` or `master` branch publish these tags to `pakalucki/daymark`:

- `latest`
- `sha-<short-commit-sha>`

Pushing a semantic version tag such as `v1.2.3` additionally publishes `1.2.3` and `1.2`.

Configure the following under **GitHub repository → Settings → Secrets and variables → Actions**:

- Repository variable `DOCKERHUB_USERNAME`: `pakalucki`
- Repository secret `DOCKERHUB_TOKEN`: a Docker Hub personal access token with **Read & Write** permission

Create the `pakalucki/daymark` repository in Docker Hub before the first publish if it does not already exist. The workflow can also be started manually from the GitHub Actions page.

## Development

The frontend is embedded in the Go binary, so no JavaScript toolchain is needed.

```sh
go test ./...
go vet ./...
```

The health endpoint is available at `GET /api/health`. SQLite runs in WAL mode, foreign keys are enabled, and event deletion cascades to that event’s calendar marks.
