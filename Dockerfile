# syntax=docker/dockerfile:1.29@sha256:572cebbc2b15e6f84c077742952674889cb63cd5e9b80abdffcbe587b6f19183
# Self-hosted GitHub Actions runners for several repositories in one container.
#
# A personal GitHub account can register a runner to one repository only, so
# the usual images need a container per repository. This one starts a runner
# for every repository you add, and a web interface on port 8080 shows them
# and manages the list. The runner itself and its tooling come from
# myoung34/github-runner.
#
# Stages:
#   ui:    node builds the web interface
#   build: a CGO-free Go build that embeds it
#   final: the pinned myoung34 image with the binary as its entrypoint
#
# GitHub:  https://github.com/junkerderprovinz/github-runner
# Image:   ghcr.io/junkerderprovinz/github-runner
# License: GPL-3.0 (as myoung34/github-runner)

# The web interface is the same for every architecture, so it is built once on
# the build host.
FROM --platform=$BUILDPLATFORM node:24.22.3-slim@sha256:873f586f3794f52281bd039bf02c048a0be08fde619338f9941bad1eb6b34530 AS ui
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.27@sha256:5f1b98fa02fe396ebce2a0227a0e08309e11792915b9dd2e1f71053d7e7e6666 AS build
ARG TARGETOS TARGETARCH
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/
COPY web/embed.go web/
COPY --from=ui /src/web/dist web/dist
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/github-runner ./cmd/github-runner

FROM myoung34/github-runner:2.338.2-ubuntu-noble@sha256:23e3a4a1da30de5b462ad0ae13d901288e803a688075c3b9b5bb79b7148c07fa

LABEL org.opencontainers.image.title="GitHub-Runner" \
      org.opencontainers.image.description="Self-hosted GitHub Actions runners for several repositories in one container" \
      org.opencontainers.image.source="https://github.com/junkerderprovinz/github-runner" \
      org.opencontainers.image.licenses="GPL-3.0" \
      org.opencontainers.image.vendor="junkerderprovinz" \
      maintainer="junkerderprovinz"

SHELL ["/bin/bash", "-o", "pipefail", "-c"]

# CR is stripped so a Windows checkout cannot break the init-log banner.
COPY .github/assets/banner-raw.txt /usr/local/share/banner-raw.txt
RUN tr -d '\r' < /usr/local/share/banner-raw.txt > /usr/local/share/banner.txt

COPY rootfs/ /
COPY --from=build /out/github-runner /usr/local/bin/github-runner
RUN chmod +x /usr/local/bin/print-banner.sh

EXPOSE 8080
VOLUME /config

# Healthy while every repository has a runner listening for jobs. The service
# knows that better than a look at the process list, and the check also fails
# when the service itself hangs.
HEALTHCHECK --interval=60s --timeout=5s --start-period=120s --retries=3 \
    CMD ["/usr/local/bin/github-runner", "health"]

# dumb-init reaps what the runners leave behind. With --single-child the stop
# signal reaches only the service, which then stops the runners in order.
ENTRYPOINT ["/usr/bin/dumb-init", "--single-child", "--", "/usr/local/bin/github-runner"]
CMD []
