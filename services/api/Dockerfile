FROM golang:1.26.4 AS build
WORKDIR /src

# Copy root module first (for replace directive)
COPY go.mod go.sum ./
COPY pkg/ ./pkg/
COPY api/ ./api/

# Copy service go.mod
COPY services/api/go.mod services/api/go.sum ./services/api/
RUN cd services/api && go mod download

# Copy service source
COPY services/api/*.go ./services/api/
COPY services/api/admin/ ./services/api/admin/
COPY services/api/auth/ ./services/api/auth/
COPY services/api/deployments/ ./services/api/deployments/
COPY services/api/identity/ ./services/api/identity/
COPY services/api/registry/ ./services/api/registry/
COPY services/api/runtime/ ./services/api/runtime/
COPY services/api/internal/ ./services/api/internal/

# Build from service directory
ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN cd services/api && \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -o /out/api .

FROM gcr.io/distroless/base-debian12
WORKDIR /app
COPY --from=build --chown=65532:65532 /out/api /app/api
EXPOSE 8080 9090
USER 65532:65532
ENTRYPOINT ["/app/api"]
