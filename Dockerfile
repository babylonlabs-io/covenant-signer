FROM golang:1.22.7-alpine as builder

# Version to build. Default is the Git HEAD.
ARG VERSION="HEAD"

# Use muslc for static libs
ARG BUILD_TAGS="muslc"

# hadolint ignore=DL3018
RUN apk add --no-cache --update openssh git make build-base linux-headers libc-dev \
                                pkgconfig zeromq-dev musl-dev alpine-sdk libsodium-dev \
                                libzmq-static libsodium-static gcc \
                                && rm -rf /var/cache/apk/*


# Build
WORKDIR /go/src/github.com/babylonlabs-io/covenant-signer
# Cache dependencies
COPY go.mod go.sum /go/src/github.com/babylonlabs-io/covenant-signer/
# Copy the rest of the files
COPY ./ /go/src/github.com/babylonlabs-io/covenant-signer/

RUN CGO_LDFLAGS="$CGO_LDFLAGS -lstdc++ -lm -lsodium" \
    CGO_ENABLED=1 \
    BUILD_TAGS=$BUILD_TAGS \
    LINK_STATICALLY=true \
    make build

# FINAL IMAGE
FROM alpine:3.20 AS run

# hadolint ignore=DL3018
RUN addgroup --gid 1138 -S covenant-signer && adduser --uid 1138 -S covenant-signer -G covenant-signer \
    && apk --no-cache add bash curl jq && rm -rf /var/cache/apk/*

COPY --from=builder /go/src/github.com/babylonlabs-io/covenant-signer/build/covenant-signer /bin/covenant-signer

WORKDIR /home/covenant-signer
RUN chown -R covenant-signer /home/covenant-signer
USER covenant-signer
