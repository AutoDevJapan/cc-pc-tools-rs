FROM golang:1.25.0-bookworm@sha256:81dc45d05a7444ead8c92a389621fafabc8e40f8fd1a19d7e5df14e61e98bc1a AS go
FROM rust:1.90.0-bookworm@sha256:3914072ca0c3b8aad871db9169a651ccfce30cf58303e5d6f2db16d1d8a7e58f
RUN apt-get update && apt-get install -y --no-install-recommends libicu72 libssl3 libkrb5-3 zlib1g libunwind8 liblttng-ust1 git ca-certificates curl jq nodejs npm && rm -rf /var/lib/apt/lists/*
COPY --from=go /usr/local/go /usr/local/go
RUN mv /usr/local/cargo /opt/rust && mv /usr/local/rustup /opt/rustup && RUSTUP_HOME=/opt/rustup CARGO_HOME=/opt/rust /opt/rust/bin/rustup component add rustfmt clippy && curl -LsSf https://get.nexte.st/0.9/linux | tar zxf - -C /opt/rust/bin && useradd -m -u 1001 runner && mkdir -p /opt/actions-runner /runner /work /opt/bridge && chown runner:runner /runner /work /opt/rust /opt/rustup
ENV CARGO_HOME=/opt/rust RUSTUP_HOME=/opt/rustup PATH=/opt/bridge:/opt/rust/bin:/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin
COPY actions-runner.tar.gz /tmp/actions-runner.tar.gz
RUN echo 'af4b794c1bc41d73d40535e3fe092a39f9679cd8d965954c2aca25a05ca41d32  /tmp/actions-runner.tar.gz' | sha256sum -c - && tar xzf /tmp/actions-runner.tar.gz -C /opt/actions-runner && rm /tmp/actions-runner.tar.gz && chown -R runner:runner /opt/actions-runner
COPY bridge /opt/bridge/bridge
RUN chmod 755 /opt/bridge/bridge && ln -s bridge /opt/bridge/cargo && printf '#!/bin/sh\nexec /opt/bridge/bridge hook\n' > /opt/bridge/job-hook.sh && chmod 755 /opt/bridge/job-hook.sh && printf '#!/bin/sh\nset -eu\ncp -a /opt/actions-runner/. /runner/\nexec sleep infinity\n' > /usr/local/bin/runner-init && chmod 755 /usr/local/bin/runner-init
USER runner
WORKDIR /runner
CMD ["/usr/local/bin/runner-init"]
