# syntax=docker/dockerfile:1.29@sha256:572cebbc2b15e6f84c077742952674889cb63cd5e9b80abdffcbe587b6f19183
# Self-hosted GitHub Actions runners for several repositories in one container.
#
# A personal GitHub account can register a runner to one repository only, so
# the usual images need a container per repository. This one starts a runner
# for every repository listed in RUNNER_REPOS. The runner itself and its
# tooling come from myoung34/github-runner.
#
# GitHub:  https://github.com/junkerderprovinz/github-runner
# Image:   ghcr.io/junkerderprovinz/github-runner
# License: GPL-3.0 (as myoung34/github-runner)

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
RUN chmod +x /usr/local/bin/start-runners.sh /usr/local/bin/print-banner.sh /usr/local/bin/healthcheck.sh

# Healthy while every listed repository has a runner listening for jobs.
HEALTHCHECK --interval=60s --timeout=5s --start-period=120s --retries=3 \
    CMD ["/usr/local/bin/healthcheck.sh"]

ENTRYPOINT ["/usr/local/bin/start-runners.sh"]
CMD []
