# syntax=docker/dockerfile:1
FROM node:22-alpine AS web
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
# vite writes to ../internal/webui/dist
RUN mkdir -p ../internal/webui && npm run build

FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY --from=web /src/internal/webui/dist ./internal/webui/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/spotis ./cmd/spotis
# distroless has no shell to mkdir with; a new named volume copies this dir's nonroot ownership
RUN mkdir -p /out/data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/spotis /spotis
COPY --from=build --chown=nonroot:nonroot /out/data /data
ENV SPOTIS_ADDR=:8080 SPOTIS_DATA_DIR=/data
VOLUME /data
EXPOSE 8080
ENTRYPOINT ["/spotis"]
