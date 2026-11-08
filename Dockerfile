ARG UBUNTU_VERSION=24.04
FROM golang:1.24 AS build
WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/daffy ./cmd/daffy

FROM ubuntu:${UBUNTU_VERSION} AS ubuntu
RUN apt-get update && apt-get install -y --no-install-recommends python3 && rm -rf /var/lib/apt/lists/* \
    && useradd --system --create-home --uid 10001 daffy
COPY --from=build /out/daffy /usr/local/bin/daffy
COPY scripts/container-smoke.sh /usr/local/bin/container-smoke
USER 10001:10001
ENTRYPOINT ["/usr/local/bin/daffy"]

# This is an Ubuntu userspace with fixture identity metadata, not a Zorin OS image.
FROM ubuntu:${UBUNTU_VERSION} AS zorin-fixture
ARG ZORIN_VERSION=18
COPY testdata/os-release-zorin-${ZORIN_VERSION} /tmp/fixture-os-release
RUN cp /tmp/fixture-os-release /etc/os-release && apt-get update && apt-get install -y --no-install-recommends python3 \
    && rm -rf /var/lib/apt/lists/* && useradd --system --create-home --uid 10001 daffy
COPY --from=build /out/daffy /usr/local/bin/daffy
COPY scripts/container-smoke.sh /usr/local/bin/container-smoke
USER 10001:10001
ENTRYPOINT ["/usr/local/bin/daffy"]
