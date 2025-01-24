# Build stage
ARG GO_VERSION=1.23.3
ARG TARGETPLATFORM="linux/amd64"

FROM --platform=$TARGETPLATFORM golang:${GO_VERSION}-alpine AS builder

ARG VERSION="0.0.0-dev"
ARG BINARY_NAME=nexus-operator

WORKDIR /app
COPY . .

RUN go mod download
RUN CGO_ENABLED=0 \
    GOOS=$(echo ${TARGETPLATFORM} | cut -d'/' -f1) \
    GOARCH=$(echo ${TARGETPLATFORM} | cut -d'/' -f2) \
    go build -ldflags "-w -X main.Version=${VERSION}" \
    -o /app/build/${BINARY_NAME} main.go

# Final image
FROM gcr.io/distroless/static:nonroot
ARG BINARY_NAME=nexus-operator
COPY --from=builder /app/build/${BINARY_NAME} /${BINARY_NAME}}
USER 65532:65532

ENTRYPOINT ["/${BINARY_NAME}}"]