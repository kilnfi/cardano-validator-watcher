#  Builder
ARG BUILDER=golang:1.27-alpine3.24@sha256:70bd7ddea8b6cbe5d6943cde279d6b67feb10cc924fb9100d1d8757c1575b280
ARG RUNNER=alpine:3.24@sha256:f59abe656b692d901e184be3445e084786602108599d2a51d229c0f5a48c71bf
ARG CARDANO_VERSION=11.1.3
ARG CNCLI_VERSION=6.8.3

FROM ${BUILDER} AS builder

ARG CARDANO_VERSION
ARG CNCLI_VERSION

WORKDIR /workspace

COPY . .

RUN apk --no-cache add gcc musl-dev

RUN go mod download \
  && go mod verify

RUN mkdir -p bin \
      && wget https://github.com/IntersectMBO/cardano-node/releases/download/${CARDANO_VERSION}/cardano-node-${CARDANO_VERSION}-linux-amd64.tar.gz -O - | tar --strip-components=2 -xvzf - ./bin/cardano-cli -C bin \
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

ENTRYPOINT ["./cardano-validator-watcher"]