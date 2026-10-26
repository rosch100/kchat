FROM golang:1.26-alpine AS builder

ARG VERSION=unknown
ARG COMMIT=unknown

RUN apk add --no-cache gcc g++ musl-dev cmake make git

# Build libolm as a static library
RUN git clone --depth 1 --branch 3.2.16 https://gitlab.matrix.org/matrix-org/olm.git /tmp/olm && \
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
