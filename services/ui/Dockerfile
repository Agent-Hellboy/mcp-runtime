FROM golang:1.26.0 AS build
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src

COPY go.mod go.sum ./
COPY pkg/ ./pkg/

COPY services/ui/go.mod services/ui/go.sum ./services/ui/
RUN cd services/ui && go mod download

COPY services/ui/*.go ./services/ui/
COPY services/ui/static/ ./services/ui/static/
RUN cd services/ui && \
    mkdir -p /out && CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} go build -o /out/ui .

FROM gcr.io/distroless/base-debian12
WORKDIR /app
COPY --from=build --chown=65532:65532 /out/ui /app/ui
EXPOSE 8082
USER 65532:65532
ENTRYPOINT ["/app/ui"]
