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
# Packages the project keeps in web/vendor/ (SheetJS publishes its current releases only as a tarball, docs/web.md「表格」).
COPY web/vendor/ ./vendor/
# A lockfile written against a mirror names the mirror's host, which no other network can reach: refuse it rather
# than fail later in npm (docs/lessons/server.md).
RUN if grep -E '"resolved": "' package-lock.json | grep -v '"file:vendor/' | grep -vq '"https://registry.npmjs.org/'; then \
      echo "web/package-lock.json resolves packages from somewhere other than registry.npmjs.org or web/vendor" >&2; exit 1; \
    fi \
 && npm ci --no-audit --no-fund
COPY web/ ./
# The shared cases a web test imports (src/lib/injected.test.ts reads ../../../spec/fixtures/injected): tsc -b in the
# build checks the tests too. Only this folder, so other fixtures' changes keep the cache.
COPY spec/fixtures/injected/ /src/spec/fixtures/injected/
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
# The extension SDK's wire types (sdk/go): server/go.mod replaces the module with ../sdk/go.
COPY sdk/go/ /src/sdk/go/
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/ ./
COPY --from=web /src/web/dist/ internal/hub/webdist/
# The runtime packages jarvisd carries and keeps its runtimes on (docs/runtimes.md「自动更新」): the DSH plugin's and
# the pi extension's, the Paseo bridge's and the Agent SDK bridge's code.
COPY integrations/agent-sdk/ /src/integrations/agent-sdk/
COPY integrations/dsh/ /src/integrations/dsh/
COPY integrations/paseo/ /src/integrations/paseo/
COPY integrations/pi/ /src/integrations/pi/
COPY integrations/runtime-sdk/ /src/integrations/runtime-sdk/
RUN go generate ./internal/runtimepkg
# NODE_PLATFORMS: the node builds in /opt/jarvis/nodes. It must include linux/<the image's architecture>, which the hub
# itself runs. The PR smoke build passes linux/amd64 only (it never starts the others); everything else builds all four.
ARG NODE_PLATFORMS="linux/amd64 linux/arm64 darwin/arm64 darwin/amd64"
RUN for t in $NODE_PLATFORMS; do \
      CGO_ENABLED=0 GOOS=${t%/*} GOARCH=${t#*/} go build -trimpath \
        -ldflags "-s -w -X main.version=$VERSION -X main.channel=docker -X main.image=$IMAGE" -o /out/nodes/jarvisd-${t%/*}-${t#*/} ./cmd/personad || exit 1; \
    done
# The built-in plugins' programs (the WeChat bot, docs/wechat.md), for this image's platform only: the hub runs them next to jarvisd, and starts one only when its `version -v` is
# jarvisd's (the same -X).
ARG TARGETARCH
RUN for p in wechat; do \
      CGO_ENABLED=0 GOOS=linux GOARCH=$TARGETARCH go build -trimpath \
        -ldflags "-s -w -X main.version=$VERSION -X main.channel=docker" -o /out/jarvisd-$p ./cmd/personad-$p || exit 1; \
    done

FROM alpine:3.22
ARG TARGETARCH
# ssh: machines the hub reaches over ssh (optional with tailcat); su-exec: the entrypoint drops to the jarvis user.
RUN apk add --no-cache ca-certificates tzdata openssh-client su-exec \
 && adduser -D -h /home/jarvis -u 1000 jarvis \
 && mkdir -p /home/jarvis/.jarvis && chown jarvis:jarvis /home/jarvis/.jarvis && chmod 700 /home/jarvis/.jarvis
COPY --from=build /out/nodes/ /opt/jarvis/nodes/
RUN ln -s /opt/jarvis/nodes/jarvisd-linux-$TARGETARCH /opt/jarvis/jarvisd && /opt/jarvis/jarvisd version
# Next to the binary jarvisd runs as (the link resolves into nodes/), where the hub looks for it.
COPY --from=build /out/jarvisd-wechat /opt/jarvis/nodes/
RUN for p in wechat; do [ "$(/opt/jarvis/nodes/jarvisd-$p version -v)" = "$(/opt/jarvis/jarvisd version -v)" ] || exit 1; done
COPY deploy/docker/jarvisd.sh /usr/local/bin/jarvisd
RUN ln -s jarvisd /usr/local/bin/personad
COPY deploy/docker/entrypoint.sh /usr/local/bin/jarvis-entrypoint
# ~/.jarvis is the data volume (hub.json, keys, the knowledge base): the node builds live outside it. No local node:
# this machine has no herdr or claude for the hub to drive.
ENV HOME=/home/jarvis \
    JARVIS_LOCAL_NODE=0 \
    JARVIS_NODE_BINARIES=/opt/jarvis/nodes
# 18660 the hub (HTTP), 18662 its TLS port (public.tls_listen: on for a first start, deploy/docker/entrypoint.sh).
EXPOSE 18660 18662
HEALTHCHECK --interval=30s --timeout=5s --start-period=60s \
  CMD wget -qO- http://127.0.0.1:18660/api/v1/health >/dev/null || exit 1
ENTRYPOINT ["jarvis-entrypoint"]
CMD ["hub", "run"]
