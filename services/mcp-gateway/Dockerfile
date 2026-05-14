FROM golang:1.26.0 AS build
WORKDIR /src

# Copy root module first to satisfy local replace directives used by the gateway module.
COPY go.mod go.sum ./
COPY pkg/ ./pkg/

# Copy service module and resolve dependencies from the service directory.
COPY services/mcp-gateway/go.mod services/mcp-gateway/go.sum ./services/mcp-gateway/
RUN cd services/mcp-gateway && go mod download

# Copy service source and build from within the service module.
COPY services/mcp-gateway/*.go ./services/mcp-gateway/
RUN cd services/mcp-gateway && \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/mcp-gateway .

FROM gcr.io/distroless/base-debian12
WORKDIR /app
COPY --from=build --chown=65532:65532 /out/mcp-gateway /app/mcp-gateway
EXPOSE 8091
USER 65532:65532
ENTRYPOINT ["/app/mcp-gateway"]
