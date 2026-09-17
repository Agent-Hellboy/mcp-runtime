FROM --platform=$BUILDPLATFORM golang:1.26.6 AS build
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY main.go ./
RUN CGO_ENABLED=0 GOOS=linux GOARCH=$TARGETARCH go build -o /out/mcp-auth-sdk-echo ./main.go

FROM gcr.io/distroless/base-debian12
COPY --from=build --chown=65532:65532 /out/mcp-auth-sdk-echo /app/mcp-auth-sdk-echo
USER 65532:65532
ENTRYPOINT ["/app/mcp-auth-sdk-echo"]
