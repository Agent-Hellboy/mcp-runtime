FROM rust:1.88-alpine AS build

RUN apk add --no-cache build-base

WORKDIR /src
COPY Cargo.toml Cargo.lock /src/
COPY src /src/src
RUN cargo build --release

FROM alpine:3.20

WORKDIR /app
COPY --from=build /src/target/release/rust-example-mcp /app/rust-example-mcp

EXPOSE 8088

ENTRYPOINT ["/app/rust-example-mcp"]
