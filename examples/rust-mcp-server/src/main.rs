use std::{net::SocketAddr, sync::Arc};

use axum::Router;
use rmcp::{
    ServerHandler,
    handler::server::{router::tool::ToolRouter, wrapper::Parameters},
    model::{ServerCapabilities, ServerInfo},
    schemars, tool, tool_handler, tool_router,
    transport::{
        streamable_http_server::session::local::LocalSessionManager,
        StreamableHttpServerConfig, StreamableHttpService,
    },
};

#[derive(Debug, serde::Deserialize, schemars::JsonSchema)]
struct MessageRequest {
    message: String,
}

#[derive(Debug, serde::Deserialize, schemars::JsonSchema)]
struct RepeatRequest {
    message: String,
    times: usize,
}

#[derive(Clone)]
struct ExampleServer {
    tool_router: ToolRouter<Self>,
}

impl ExampleServer {
    fn new() -> Self {
        Self {
            tool_router: Self::tool_router(),
        }
    }
}

impl Default for ExampleServer {
    fn default() -> Self {
        Self::new()
    }
}

#[tool_router(router = tool_router)]
impl ExampleServer {
    #[tool(description = "Repeat the provided message a number of times")]
    fn repeat(&self, Parameters(RepeatRequest { message, times }): Parameters<RepeatRequest>) -> String {
        message.repeat(times)
    }

    #[tool(description = "Count the words in the provided message")]
    fn word_count(&self, Parameters(MessageRequest { message }): Parameters<MessageRequest>) -> String {
        message.split_whitespace().count().to_string()
    }
}

#[tool_handler(router = self.tool_router)]
impl ServerHandler for ExampleServer {
    fn get_info(&self) -> ServerInfo {
        ServerInfo::new(ServerCapabilities::builder().enable_tools().build())
            .with_instructions("A Rust MCP example server built with the official rmcp SDK")
    }
}

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    let port = std::env::var("PORT")
        .ok()
        .and_then(|value| value.parse::<u16>().ok())
        .unwrap_or(8088);
    let mcp_path = std::env::var("MCP_PATH").unwrap_or_else(|_| "/mcp".to_string());

    let mcp_service = StreamableHttpService::new(
        || Ok(ExampleServer::new()),
        Arc::new(LocalSessionManager::default()),
        // disable_allowed_hosts clears the default allowlist (localhost/127.0.0.1/::1)
        // so requests forwarded by Traefik with an arbitrary Host header are accepted.
        StreamableHttpServerConfig::default().disable_allowed_hosts(),
    );

    let app = Router::new().nest_service(&mcp_path, mcp_service);
    let address = SocketAddr::from(([0, 0, 0, 0], port));
    let listener = tokio::net::TcpListener::bind(address).await?;

    axum::serve(listener, app).await?;
    Ok(())
}
