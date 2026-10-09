# Сборка
ARG GO_VERSION=1.23.3

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine AS builder

ARG VERSION="0.0.0-dev"
ARG TARGETOS
ARG TARGETARCH

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags "-s -w -X main.Version=${VERSION}" \
    -o /app/build/nexus-operator main.go

# Итоговый образ
FROM gcr.io/distroless/static:nonroot

COPY --from=builder /app/build/nexus-operator /nexus-operator
USER 65532:65532

ENTRYPOINT ["/nexus-operator"]
