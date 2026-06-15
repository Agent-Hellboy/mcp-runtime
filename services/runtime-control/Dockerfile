FROM golang:1.26.4 AS build
WORKDIR /src

COPY go.mod go.sum ./
COPY pkg/ ./pkg/
COPY api/ ./api/

COPY services/runtime-control/go.mod services/runtime-control/go.sum ./services/runtime-control/
RUN cd services/runtime-control && go mod download

COPY services/runtime-control/ ./services/runtime-control/

ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN cd services/runtime-control && \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -o /out/runtime-control .

FROM gcr.io/distroless/base-debian12
WORKDIR /app
COPY --from=build --chown=65532:65532 /out/runtime-control /app/runtime-control
EXPOSE 8084 9094
USER 65532:65532
ENTRYPOINT ["/app/runtime-control"]
