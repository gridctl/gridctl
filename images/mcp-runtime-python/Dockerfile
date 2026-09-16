# MCP Python runtime base. Not a ready-to-run MCP server and not the Gridctl gateway.
FROM python:3.12.11-slim-bookworm@sha256:519591d6871b7bc437060736b9f7456b8731f1499a57e22e6c285135ae657bf7

LABEL org.opencontainers.image.title="gridctl MCP Python runtime base" \
      org.opencontainers.image.description="Python 3.12 runtime foundation for derived MCP servers. Not a ready MCP server." \
      org.opencontainers.image.source="https://github.com/gridctl/gridctl" \
      org.opencontainers.image.licenses="Apache-2.0"

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    PYTHONFAULTHANDLER=1 \
    HOME=/tmp \
    XDG_CACHE_HOME=/tmp/cache \
    XDG_CONFIG_HOME=/tmp/config \
    XDG_DATA_HOME=/data \
    PATH="/app/.venv/bin:/usr/local/bin:$PATH"

COPY NOTICE /usr/share/doc/gridctl-mcp-runtime-python/NOTICE

ARG DEBIAN_SNAPSHOT=20250901T000000Z
ARG TINI_VERSION=0.19.0-1
RUN printf '%s\n' \
        "Types: deb" \
        "URIs: https://snapshot.debian.org/archive/debian/${DEBIAN_SNAPSHOT}" \
        "Suites: bookworm" \
        "Components: main" \
        "Signed-By: /usr/share/keyrings/debian-archive-keyring.gpg" \
        "Check-Valid-Until: no" \
        > /etc/apt/sources.list.d/debian.sources \
    && rm -f /etc/apt/sources.list \
    && apt-get update \
    && apt-get install -y --no-install-recommends tini=${TINI_VERSION} \
    && test "$(dpkg-query -W -f='${Version}' tini)" = "${TINI_VERSION}" \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --gid 10001 gridctl \
    && useradd --uid 10001 --gid 10001 --home-dir /tmp --no-create-home --shell /usr/sbin/nologin gridctl \
    && mkdir -p /app /data \
    && chmod 0755 /app /data \
    && chown root:root /app /data

WORKDIR /app
USER 10001:10001
STOPSIGNAL SIGTERM
ENTRYPOINT ["/usr/bin/tini", "--"]
CMD ["python", "-c", "raise SystemExit('mcp-runtime-python is a runtime base, not an MCP server')"]
