//! Shared generated runtime for sdkgen clients: the canonical error model.

/// Canonical SDK error codes, drawn from the standard gRPC status codes.
pub mod gestalt_error_code {
    /// The operation was cancelled.
    pub const CANCELLED: i32 = 1;
    /// The cause of the error is unknown.
    pub const UNKNOWN: i32 = 2;
    /// The client specified an invalid argument.
    pub const INVALID_ARGUMENT: i32 = 3;
    /// The deadline expired before the operation could complete.
    pub const DEADLINE_EXCEEDED: i32 = 4;
    /// The requested entity was not found.
    pub const NOT_FOUND: i32 = 5;
    /// The entity the client attempted to create already exists.
    pub const ALREADY_EXISTS: i32 = 6;
    /// The caller does not have permission to execute the operation.
    pub const PERMISSION_DENIED: i32 = 7;
    /// A resource has been exhausted.
    pub const RESOURCE_EXHAUSTED: i32 = 8;
    /// The system is not in a state required for the operation.
    pub const FAILED_PRECONDITION: i32 = 9;
    /// The operation was aborted.
    pub const ABORTED: i32 = 10;
    /// The operation was attempted past the valid range.
    pub const OUT_OF_RANGE: i32 = 11;
    /// The operation is not implemented or supported.
    pub const UNIMPLEMENTED: i32 = 12;
    /// An internal error occurred.
    pub const INTERNAL: i32 = 13;
    /// The service is currently unavailable.
    pub const UNAVAILABLE: i32 = 14;
    /// Unrecoverable data loss or corruption.
    pub const DATA_LOSS: i32 = 15;
    /// The request lacks valid authentication credentials.
    pub const UNAUTHENTICATED: i32 = 16;
}

/// Canonical SDK error: one numeric status code and a human-readable message.
/// Concrete transport adapters map their native errors into this type.
#[derive(Debug, thiserror::Error)]
#[error("{message}")]
pub struct GestaltError {
    /// Numeric status code, one of the gestalt_error_code constants.
    pub code: i32,
    /// Human-readable error message.
    pub message: String,
}

impl GestaltError {
    /// Creates an error with the given code and message.
    pub fn new(code: i32, message: impl Into<String>) -> Self {
        Self {
            code,
            message: message.into(),
        }
    }
}

/// Maps an HTTP status code to the canonical Gestalt gRPC error code.
pub fn http_status_to_gestalt_code(status: i32) -> i32 {
    match status {
        400 => gestalt_error_code::INVALID_ARGUMENT,
        401 => gestalt_error_code::UNAUTHENTICATED,
        403 => gestalt_error_code::PERMISSION_DENIED,
        404 => gestalt_error_code::NOT_FOUND,
        409 => gestalt_error_code::ALREADY_EXISTS,
        412 => gestalt_error_code::FAILED_PRECONDITION,
        429 => gestalt_error_code::RESOURCE_EXHAUSTED,
        499 => gestalt_error_code::CANCELLED,
        500 => gestalt_error_code::INTERNAL,
        501 => gestalt_error_code::UNIMPLEMENTED,
        503 => gestalt_error_code::UNAVAILABLE,
        504 => gestalt_error_code::DEADLINE_EXCEEDED,
        _ => gestalt_error_code::UNKNOWN,
    }
}
