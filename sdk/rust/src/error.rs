#[derive(Clone, Debug, Eq, PartialEq, thiserror::Error)]
#[error("{message}")]
/// Error returned by typed provider handlers and runtime helpers.
pub struct Error {
    status: Option<u16>,
    message: String,
    expose_message: bool,
}

pub(crate) const HTTP_BAD_REQUEST: u16 = 400;
pub(crate) const HTTP_UNAUTHORIZED: u16 = 401;
pub(crate) const HTTP_FORBIDDEN: u16 = 403;
pub(crate) const HTTP_NOT_FOUND: u16 = 404;
pub(crate) const HTTP_CONFLICT: u16 = 409;
pub(crate) const HTTP_PRECONDITION_FAILED: u16 = 412;
pub(crate) const HTTP_INTERNAL_SERVER_ERROR: u16 = 500;
pub(crate) const HTTP_NOT_IMPLEMENTED: u16 = 501;
pub(crate) const HTTP_UNAVAILABLE: u16 = 503;
pub(crate) const INTERNAL_ERROR_MESSAGE: &str = "internal error";

/// Convenient result alias for Gestalt SDK operations.
pub type Result<T> = std::result::Result<T, Error>;

impl Error {
    /// Creates an error without an explicit HTTP status code override.
    pub fn new(message: impl Into<String>) -> Self {
        Self {
            status: None,
            message: message.into(),
            expose_message: true,
        }
    }

    /// Creates an error with an explicit HTTP status code.
    pub fn with_status(status: u16, message: impl Into<String>) -> Self {
        Self {
            status: Some(status),
            message: message.into(),
            expose_message: true,
        }
    }

    /// Creates a `400 Bad Request` error.
    pub fn bad_request(message: impl Into<String>) -> Self {
        Self::with_status(HTTP_BAD_REQUEST, message)
    }

    /// Creates a `500 Internal Server Error`.
    pub fn internal(message: impl Into<String>) -> Self {
        Self::with_status(HTTP_INTERNAL_SERVER_ERROR, message)
    }

    /// Creates a `404 Not Found` error.
    pub fn not_found(message: impl Into<String>) -> Self {
        Self::with_status(HTTP_NOT_FOUND, message)
    }

    /// Creates a `409 Conflict` error for resources that already exist.
    pub fn already_exists(message: impl Into<String>) -> Self {
        Self::with_status(HTTP_CONFLICT, message)
    }

    /// Creates a `412 Precondition Failed` error.
    pub fn failed_precondition(message: impl Into<String>) -> Self {
        Self::with_status(HTTP_PRECONDITION_FAILED, message)
    }

    /// Creates a `503 Service Unavailable` error.
    pub fn unavailable(message: impl Into<String>) -> Self {
        Self::with_status(HTTP_UNAVAILABLE, message)
    }

    /// Creates a `401 Unauthorized` error.
    pub fn unauthenticated(message: impl Into<String>) -> Self {
        Self::with_status(HTTP_UNAUTHORIZED, message)
    }

    /// Creates a `403 Forbidden` error.
    pub fn permission_denied(message: impl Into<String>) -> Self {
        Self::with_status(HTTP_FORBIDDEN, message)
    }

    /// Creates a `501 Not Implemented` error.
    pub fn unimplemented(message: impl Into<String>) -> Self {
        Self::with_status(HTTP_NOT_IMPLEMENTED, message)
    }

    /// Returns the HTTP status code that should be used for this error, when
    /// one was supplied.
    pub fn status(&self) -> Option<u16> {
        self.status
    }

    /// Returns the human-readable error message.
    pub fn message(&self) -> &str {
        &self.message
    }

    pub(crate) fn expose_message(&self) -> bool {
        self.expose_message
    }

    pub(crate) fn hidden_internal(message: impl Into<String>) -> Self {
        Self {
            status: Some(HTTP_INTERNAL_SERVER_ERROR),
            message: message.into(),
            expose_message: false,
        }
    }
}

impl From<serde_json::Error> for Error {
    fn from(value: serde_json::Error) -> Self {
        Self::hidden_internal(value.to_string())
    }
}

impl From<std::io::Error> for Error {
    fn from(value: std::io::Error) -> Self {
        Self::hidden_internal(value.to_string())
    }
}

impl From<tonic::transport::Error> for Error {
    fn from(value: tonic::transport::Error) -> Self {
        Self::hidden_internal(value.to_string())
    }
}
