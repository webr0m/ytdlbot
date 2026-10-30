FROM node:24-bookworm-slim AS web
WORKDIR /src/web
COPY web/package*.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.27-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/internal/app/static ./internal/app/static
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /app ./cmd/server

FROM node:24-bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends python3 python3-venv ffmpeg ca-certificates && python3 -m venv /opt/engines && /opt/engines/bin/pip install --no-cache-dir 'yt-dlp[default]' gallery-dl && rm -rf /var/lib/apt/lists/*
ENV PATH="/opt/engines/bin:${PATH}"
WORKDIR /app
COPY --from=build /app /usr/local/bin/webrom-downloader
RUN useradd -m -u 10001 downloader && mkdir /data && chown downloader:downloader /data
USER downloader
EXPOSE 8095
CMD ["webrom-downloader"]
