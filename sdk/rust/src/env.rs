/// Unified host-service socket env var for app-side clients.
pub const ENV_HOST_SERVICE_SOCKET: &str = "GESTALT_HOST_SERVICE_SOCKET";
/// Unified host-service relay token env var for app-side clients.
pub const ENV_HOST_SERVICE_TOKEN: &str = "GESTALT_HOST_SERVICE_TOKEN";
/// gRPC metadata header naming the host binding for routed services.
pub const HOST_SERVICE_BINDING_HEADER: &str = "x-gestalt-host-binding";
/// Unix socket path exposed by `gestaltd` for the main integration-provider
/// surface.
pub const ENV_PROVIDER_SOCKET: &str = "GESTALT_PROVIDER_SOCKET";
/// Parent process id used for lifecycle shutdown detection.
pub const ENV_PROVIDER_PARENT_PID: &str = "GESTALT_APP_PARENT_PID";
/// Optional path where the runtime should write the derived static catalog.
pub const ENV_WRITE_CATALOG: &str = "GESTALT_APP_WRITE_CATALOG";
/// Provider name override supplied by the host runtime.
pub const ENV_PROVIDER_NAME: &str = "GESTALT_APP_NAME";
/// Current Gestalt provider protocol version spoken by this SDK.
pub const CURRENT_PROTOCOL_VERSION: i32 = 5;
