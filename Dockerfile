# Local-testing image for the Warpgate daemon. Not used in production.
FROM golang:1.25 AS build
WORKDIR /src
COPY . .
# go build fetches only imported modules, which keeps the private
# agent-resources tool dependency out of the container build.
RUN --mount=type=cache,target=/go/pkg/mod \
	--mount=type=cache,target=/root/.cache/go-build \
	CGO_ENABLED=0 go build -o /out/warpgate ./cmd/warpgate

# The Turso driver loads a glibc shared library at runtime and the deploy
# engine shells out to the ssh client, so the runtime base stays glibc Debian.
FROM debian:bookworm-slim
RUN apt-get update \
	&& apt-get install -y --no-install-recommends ca-certificates openssh-client \
	&& rm -rf /var/lib/apt/lists/*
COPY --from=build /out/warpgate /usr/local/bin/warpgate

ENV WARPGATE_DB_PATH=/var/lib/warpgate/warpgate.db \
	WARPGATE_HOST_KEY=/var/lib/warpgate/host_key \
	WARPGATE_HTTP_ADDR=0.0.0.0:7411 \
	WARPGATE_SSH_ADDR=0.0.0.0:7422

RUN mkdir -p /var/lib/warpgate
VOLUME /var/lib/warpgate
EXPOSE 7411 7422

ENTRYPOINT ["warpgate"]
CMD ["serve"]
