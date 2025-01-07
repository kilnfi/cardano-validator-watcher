#  Builder
ARG BUILDER=golang:1.23-alpine3.19
ARG RUNNER=alpine:3.19
ARG CARDANO_VERSION=10.1.3
ARG CNCLI_VERSION=6.5.1

FROM ${BUILDER} AS builder

ARG CARDANO_VERSION
ARG CNCLI_VERSION

WORKDIR /workspace

COPY . .

RUN apk --no-cache add gcc musl-dev

RUN go mod download \
  && go mod verify

RUN mkdir -p bin \
      && wget https://github.com/IntersectMBO/cardano-node/releases/download/${CARDANO_VERSION}/cardano-node-${CARDANO_VERSION}-linux.tar.gz -O - | tar --strip-components=2 -xvzf - ./bin/cardano-cli -C bin \
      && wget https://github.com/cardano-community/cncli/releases/download/v${CNCLI_VERSION}/cncli-${CNCLI_VERSION}-ubuntu22-x86_64-unknown-linux-musl.tar.gz -O - | tar -xvzf - -C bin \
      && chmod +x ./bin/cncli

ENV CGO_ENABLED=1
RUN go build -v -o /usr/local/bin/cardano-validator-watcher cmd/watcher/main.go

FROM ${RUNNER}

WORKDIR /home/cardano

RUN apk --no-cache add ca-certificates curl sqlite \
  && update-ca-certificates

COPY --from=builder /usr/local/bin/cardano-validator-watcher .
COPY --from=builder /workspace/bin /usr/local/bin

ENTRYPOINT ["/home/cardano/cardano-validator-watcher"]