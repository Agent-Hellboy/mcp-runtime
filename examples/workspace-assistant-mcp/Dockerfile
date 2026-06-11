FROM golang:1.26.0 AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . ./
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/workspace-assistant-mcp ./main.go

FROM gcr.io/distroless/base-debian12
WORKDIR /app
COPY --from=build --chown=65532:65532 /out/workspace-assistant-mcp /app/workspace-assistant-mcp
EXPOSE 8088
USER 65532:65532
ENTRYPOINT ["/app/workspace-assistant-mcp"]
