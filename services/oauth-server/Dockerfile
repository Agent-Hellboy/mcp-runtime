FROM golang:1.26.6 AS build
WORKDIR /src

COPY services/oauth-server/go.mod services/oauth-server/go.sum ./services/oauth-server/
RUN cd services/oauth-server && go mod download
COPY services/oauth-server/ ./services/oauth-server/
ARG TARGETOS
ARG TARGETARCH
RUN cd services/oauth-server && \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -o /out/oauth-server .

FROM gcr.io/distroless/base-debian12
WORKDIR /app
COPY --from=build --chown=65532:65532 /out/oauth-server /app/oauth-server
EXPOSE 8086
USER 65532:65532
ENTRYPOINT ["/app/oauth-server"]
