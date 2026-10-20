# syntax=docker/dockerfile:1
# Build stages run on the builder's native platform and cross-compile, so
# multi-arch images need no QEMU emulation.
FROM --platform=$BUILDPLATFORM node:22-alpine AS web
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
COPY --from=web /web/dist ./web/dist
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/spotify-proxy . \
 && mkdir /out/data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/spotify-proxy /spotify-proxy
COPY --from=build --chown=65532:65532 /out/data /data
ENV DATA_DIR=/data LISTEN=:8080
VOLUME /data
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s CMD ["/spotify-proxy", "healthcheck"]
ENTRYPOINT ["/spotify-proxy"]
