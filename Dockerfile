FROM golang:1.26-alpine@sha256:27f2d3b83cfc5803c8078f8e730af0f7c431bf067afaec8f60a36fb763187cba AS builder

ARG VERSION=unknown
ARG COMMIT=unknown

RUN apk add --no-cache gcc g++ musl-dev cmake make git

# Build libolm as a static library
RUN git init /tmp/olm && \
    git -C /tmp/olm remote add origin https://gitlab.matrix.org/matrix-org/olm.git && \
    git -C /tmp/olm fetch --depth 1 origin 7e0c8277032e40308987257b711b38af8d77cc69 && \
    git -C /tmp/olm checkout --detach 7e0c8277032e40308987257b711b38af8d77cc69 && \
    cd /tmp/olm && \
    cmake -B build -DBUILD_SHARED_LIBS=OFF -DCMAKE_INSTALL_PREFIX=/usr -DCMAKE_POLICY_VERSION_MINIMUM=3.5 && \
    cmake --build build --parallel && \
    cmake --install build && \
    rm -rf /tmp/olm

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -ldflags "-s -w \
    -linkmode external -extldflags '-static' \
    -X main.Tag=${VERSION} \
    -X main.Commit=${COMMIT} \
    -X 'main.BuildTime=$(date -Iseconds)'" \
    -o /matrix-kchat ./cmd/matrix-kchat

# Binary-only export stage (used by CI to extract the binary)
FROM scratch AS binary
COPY --from=builder /matrix-kchat /matrix-kchat

# Runtime image
FROM scratch AS runtime

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /matrix-kchat /usr/bin/matrix-kchat

VOLUME /data
WORKDIR /data

EXPOSE 29325

ENTRYPOINT ["/usr/bin/matrix-kchat"]
CMD ["-c", "/data/config.yaml"]
