#[derive(Clone, Debug, Eq, PartialEq, thiserror::Error)]
#[error("{message}")]
pub struct Error {
    status: Option<u16>,
    message: String,
}

pub type Result<T> = std::result::Result<T, Error>;

impl Error {
    pub fn new(message: impl Into<String>) -> Self {
        Self {
            status: None,
            message: message.into(),
        }
    }

    pub fn with_status(status: u16, message: impl Into<String>) -> Self {
        Self {
            status: Some(status),
            message: message.into(),
        }
    }

    pub fn bad_request(message: impl Into<String>) -> Self {
        Self::with_status(http::StatusCode::BAD_REQUEST.as_u16(), message)
    }

    pub fn internal(message: impl Into<String>) -> Self {
        Self::with_status(http::StatusCode::INTERNAL_SERVER_ERROR.as_u16(), message)
    }

    pub fn not_found(message: impl Into<String>) -> Self {
        Self::with_status(http::StatusCode::NOT_FOUND.as_u16(), message)
    }

    pub fn unimplemented(message: impl Into<String>) -> Self {
        Self::with_status(http::StatusCode::NOT_IMPLEMENTED.as_u16(), message)
    }

    pub fn status(&self) -> Option<u16> {
        self.status
    }

    pub fn message(&self) -> &str {
        &self.message
    }
}

impl From<serde_json::Error> for Error {
    fn from(value: serde_json::Error) -> Self {
        Self::internal(value.to_string())
    }
}

impl From<serde_yaml::Error> for Error {
    fn from(value: serde_yaml::Error) -> Self {
        Self::internal(value.to_string())
    }
}

impl From<std::io::Error> for Error {
    fn from(value: std::io::Error) -> Self {
        Self::internal(value.to_string())
    }
}

impl From<tonic::transport::Error> for Error {
    fn from(value: tonic::transport::Error) -> Self {
        Self::internal(value.to_string())
    }
}
