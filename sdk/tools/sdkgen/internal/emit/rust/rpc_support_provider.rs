// Provider-only extensions to the shared generated error model.

/// Native representation of google.rpc.Status carried in response payloads,
/// mirroring the canonical error model.
#[derive(Clone, Debug, Default, Eq, PartialEq)]
pub struct RpcStatus {
    /// Numeric gRPC status code, one of the gestalt_error_code constants.
    pub code: i32,
    /// Human-readable error message.
    pub message: String,
}

impl From<tonic::Status> for GestaltError {
    fn from(status: tonic::Status) -> Self {
        // tonic reports an expired grpc-timeout as CANCELLED carrying the
        // TimeoutExpired message; the gRPC spec assigns deadline expiry
        // DEADLINE_EXCEEDED, so the canonical code is restored here.
        let timeout_expired = tonic::TimeoutExpired(()).to_string();
        let code = if status.code() == tonic::Code::Cancelled && status.message() == timeout_expired
        {
            gestalt_error_code::DEADLINE_EXCEEDED
        } else {
            status.code() as i32
        };
        Self {
            code,
            message: status.message().to_string(),
        }
    }
}
