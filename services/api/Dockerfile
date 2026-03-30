FROM golang:1.24 AS build
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

# Build from service directory
RUN cd services/api && \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/api .

FROM gcr.io/distroless/base-debian12
WORKDIR /app
COPY --from=build --chown=65532:65532 /out/api /app/api
EXPOSE 8080 9090
USER 65532:65532
ENTRYPOINT ["/app/api"]
