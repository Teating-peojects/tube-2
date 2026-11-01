# syntax=docker/dockerfile:1
FROM golang:1.22-alpine AS build
WORKDIR /src
COPY . .
# First build resolves go.sum; commit the generated go.sum afterwards for reproducible builds.
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod tidy && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/api /api
USER nonroot:nonroot
EXPOSE 8080
HEALTHCHECK --interval=15s --timeout=4s --start-period=10s --retries=3 CMD ["/api", "healthcheck"]
ENTRYPOINT ["/api"]
