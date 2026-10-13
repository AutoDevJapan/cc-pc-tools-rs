FROM golang:1.25.0-bookworm AS go
FROM rust:1.90.0-bookworm
RUN apt-get update && apt-get install -y --no-install-recommends libicu72 libssl3 libkrb5-3 zlib1g libunwind8 liblttng-ust1 git ca-certificates curl jq nodejs npm && rm -rf /var/lib/apt/lists/*
COPY --from=go /usr/local/go /usr/local/go
RUN mv /usr/local/cargo /opt/rust && mv /usr/local/rustup /opt/rustup && useradd -m -u 1001 runner && mkdir -p /runner /work /opt/bridge && chown runner:runner /runner /work /opt/rust /opt/rustup
ENV CARGO_HOME=/opt/rust RUSTUP_HOME=/opt/rustup PATH=/opt/bridge:/opt/rust/bin:/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin
COPY actions-runner.tar.gz /tmp/actions-runner.tar.gz
RUN echo 'af4b794c1bc41d73d40535e3fe092a39f9679cd8d965954c2aca25a05ca41d32  /tmp/actions-runner.tar.gz' | sha256sum -c - && tar xzf /tmp/actions-runner.tar.gz -C /runner && rm /tmp/actions-runner.tar.gz && chown -R runner:runner /runner
COPY bridge /opt/bridge/bridge
RUN chmod 755 /opt/bridge/bridge && ln -s bridge /opt/bridge/cargo && printf '#!/bin/sh\nexec /opt/bridge/bridge hook\n' > /opt/bridge/job-hook && chmod 755 /opt/bridge/job-hook
USER runner
WORKDIR /runner
CMD ["sleep","infinity"]
