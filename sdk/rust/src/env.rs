/// Unix socket path exposed by `gestaltd` for the main integration-provider
/// surface.
pub const ENV_PROVIDER_SOCKET: &str = "GESTALT_PLUGIN_SOCKET";
/// Parent process id used for lifecycle shutdown detection.
pub const ENV_PROVIDER_PARENT_PID: &str = "GESTALT_PLUGIN_PARENT_PID";
/// Optional path where the runtime should write the derived static catalog.
pub(crate) const ENV_WRITE_CATALOG: &str = "GESTALT_PLUGIN_WRITE_CATALOG";
/// Optional path where the runtime should write generated manifest metadata.
pub(crate) const ENV_WRITE_MANIFEST_METADATA: &str = "GESTALT_PLUGIN_WRITE_MANIFEST_METADATA";
/// Provider name override supplied by the host runtime.
pub const ENV_PROVIDER_NAME: &str = "GESTALT_PLUGIN_NAME";
/// Current Gestalt provider protocol version spoken by this SDK.
pub const CURRENT_PROTOCOL_VERSION: i32 = 3;
