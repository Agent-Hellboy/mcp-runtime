FROM golang:1.24 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . ./
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/mcp-server ./main.go

FROM gcr.io/distroless/base-debian12
WORKDIR /app
COPY --from=build --chown=65532:65532 /out/mcp-server /app/mcp-server
EXPOSE 8090
USER 65532:65532
ENTRYPOINT ["/app/mcp-server"]
