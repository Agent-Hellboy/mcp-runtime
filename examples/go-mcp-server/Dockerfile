FROM golang:1.25.9 AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . ./
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/go-mcp-server ./main.go

FROM gcr.io/distroless/base-debian12
WORKDIR /app
COPY --from=build --chown=65532:65532 /out/go-mcp-server /app/go-mcp-server
EXPOSE 8088
USER 65532:65532
ENTRYPOINT ["/app/go-mcp-server"]
