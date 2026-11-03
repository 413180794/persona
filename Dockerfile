# The hub as a container image (docs/docker.md): jarvisd hub with the web client inside, and every node build for the
# app's 重新部署. Built for linux/amd64 and linux/arm64 (.github/workflows/hub-image.yml); the binaries are cross
# compiled on the build machine, only the last stage runs on the target platform.
#
#   docker build -t jarvis-hub .
#
# No skills are inside: the assistant's come from ~/.jarvis/skills on its machine (docs/hub-update.md「skill 从二进制里拿出来」).

FROM --platform=$BUILDPLATFORM node:22-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
# A lockfile written against a mirror names the mirror's host, which no other network can reach: refuse it rather
# than fail later in npm (docs/lessons/server.md).
RUN if grep -E '"resolved": "' package-lock.json | grep -vq '"https://registry.npmjs.org/'; then \
      echo "web/package-lock.json resolves packages from somewhere other than registry.npmjs.org" >&2; exit 1; \
    fi \
 && npm ci --no-audit --no-fund
COPY web/ ./
# Without prebuild's privacy check: it scans the whole checkout (scripts/, git), which is not in this context. It
# runs on the checkout before the image is built (.github/workflows/hub-image.yml; locally the hooks and web builds).
RUN npm run build --ignore-scripts

FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build
# VERSION: what the hub reports and compares with its nodes' (a node of another build gets 重新部署). The image's
# hub never replaces itself (channel docker): it says when a newer image is out (docs/hub-update.md), looking up the
# tags of IMAGE (the workflow passes ghcr.io/<owner>/<repo>) unless the compose file says which image it runs.
ARG VERSION=dev
ARG IMAGE=
WORKDIR /src/server
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/ ./
COPY --from=web /src/web/dist/ internal/hub/webdist/
# The runtime packages jarvisd carries and keeps its runtimes on (docs/runtimes.md「自动更新」): the DSH plugin's and
# the pi extension's code.
COPY integrations/dsh/ /src/integrations/dsh/
COPY integrations/pi/ /src/integrations/pi/
COPY integrations/runtime-sdk/ /src/integrations/runtime-sdk/
RUN go generate ./internal/runtimepkg
RUN for t in linux/amd64 linux/arm64 darwin/arm64 darwin/amd64; do \
      CGO_ENABLED=0 GOOS=${t%/*} GOARCH=${t#*/} go build -trimpath \
        -ldflags "-s -w -X main.version=$VERSION -X main.channel=docker -X main.image=$IMAGE" -o /out/nodes/jarvisd-${t%/*}-${t#*/} ./cmd/personad || exit 1; \
    done
# The Telegram connector's program (docs/plan-telegram.md), for this image's platform only: the hub runs it next to
# jarvisd, and starts it only when its `version -v` is jarvisd's (the same -X).
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=linux GOARCH=$TARGETARCH go build -trimpath \
      -ldflags "-s -w -X main.version=$VERSION -X main.channel=docker" -o /out/jarvisd-telegram ./cmd/personad-telegram

FROM alpine:3.22
ARG TARGETARCH
# ssh: machines the hub reaches over ssh (optional with tailcat); su-exec: the entrypoint drops to the jarvis user.
RUN apk add --no-cache ca-certificates tzdata openssh-client su-exec \
 && adduser -D -h /home/jarvis -u 1000 jarvis \
 && mkdir -p /home/jarvis/.jarvis && chown jarvis:jarvis /home/jarvis/.jarvis && chmod 700 /home/jarvis/.jarvis
COPY --from=build /out/nodes/ /opt/jarvis/nodes/
RUN ln -s /opt/jarvis/nodes/jarvisd-linux-$TARGETARCH /opt/jarvis/jarvisd && /opt/jarvis/jarvisd version
# Next to the binary jarvisd runs as (the link resolves into nodes/), where the hub looks for it.
COPY --from=build /out/jarvisd-telegram /opt/jarvis/nodes/jarvisd-telegram
RUN [ "$(/opt/jarvis/nodes/jarvisd-telegram version -v)" = "$(/opt/jarvis/jarvisd version -v)" ]
COPY deploy/docker/jarvisd.sh /usr/local/bin/jarvisd
RUN ln -s jarvisd /usr/local/bin/personad
COPY deploy/docker/entrypoint.sh /usr/local/bin/jarvis-entrypoint
# ~/.jarvis is the data volume (hub.json, keys, the knowledge base): the node builds live outside it. No local node:
# this machine has no herdr or claude for the hub to drive.
ENV HOME=/home/jarvis \
    JARVIS_LOCAL_NODE=0 \
    JARVIS_NODE_BINARIES=/opt/jarvis/nodes
EXPOSE 18660
HEALTHCHECK --interval=30s --timeout=5s --start-period=60s \
  CMD wget -qO- http://127.0.0.1:18660/api/v1/health >/dev/null || exit 1
ENTRYPOINT ["jarvis-entrypoint"]
CMD ["hub", "run"]
