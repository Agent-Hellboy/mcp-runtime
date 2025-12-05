FROM golang:1.22-alpine AS build
ENV CGO_ENABLED=0 GO111MODULE=off
WORKDIR /src
COPY main.go .
RUN go build -o server main.go

FROM alpine:3.19
RUN addgroup -g 1000 appuser && adduser -D -u 1000 -G appuser appuser
WORKDIR /app
COPY --from=build /src/server /app/server
RUN chown -R appuser:appuser /app
USER appuser
EXPOSE 8088
ENTRYPOINT ["/app/server"]
