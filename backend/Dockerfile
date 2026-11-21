# syntax=docker/dockerfile:1
# One image per process: docker build --build-arg APP=relay .
ARG GO_VERSION=1.27.4

FROM golang:${GO_VERSION}-alpine AS build
WORKDIR /src
RUN apk add --no-cache git ca-certificates tzdata
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
ARG APP=api
ARG VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -trimpath \
    -ldflags="-s -w -X github.com/SyDuc7421/webhook-delivery-platform/backend/cmd/internal/boot.Version=${VERSION}" \
    -o /out/app ./cmd/${APP}

FROM gcr.io/distroless/static-debian13:nonroot AS runtime
COPY --from=build /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /out/app /app
USER nonroot:nonroot
EXPOSE 8080 9100
ENTRYPOINT ["/app"]

