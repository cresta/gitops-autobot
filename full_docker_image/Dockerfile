# Contains a large number of scripts and extras for go, helm, and other additions we need
FROM golang:1.16.0-buster AS builder
# hadolint ignore=DL3008
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
    xz-utils zip && \
    rm -rf /var/lib/apt/lists/*
WORKDIR /work

# Install mage
ARG MAGE_VERSION=1.11.0
RUN curl -L -o /tmp/mage.tar.gz "https://github.com/magefile/mage/releases/download/v${MAGE_VERSION}/mage_${MAGE_VERSION}_Linux-64bit.tar.gz" && tar -C /tmp -zxvf /tmp/mage.tar.gz && mv /tmp/mage /usr/local/bin

COPY ../go.mod /work
COPY ../go.sum /work
RUN go mod download
RUN go mod verify
COPY .. /work
RUN mage go:build
RUN mkdir /empty_dir

FROM ubuntu:20.04
LABEL org.opencontainers.image.source=https://github.com/cresta/gitops-autobot
COPY --from=builder /work/main /main
RUN apt-get update && apt-get install -y wget git
RUN cd /tmp && wget --progress=dot:giga https://golang.org/dl/go1.16.7.linux-amd64.tar.gz && tar -xf go1.16.7.linux-amd64.tar.gz && mv /tmp/go /bin
ENV PATH=$PATH:/bin/go/bin
RUN go version
COPY ./full_docker_image /gitops-autobot
EXPOSE 8080
ENTRYPOINT ["/main"]
