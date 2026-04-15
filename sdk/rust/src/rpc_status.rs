use tonic::Status;

use crate::Error;
use crate::error::INTERNAL_ERROR_MESSAGE;

pub(crate) fn rpc_error_message(operation: &str, error: &Error) -> String {
    if !error.expose_message() {
        eprintln!("internal error in Gestalt {operation}: {}", error.message());
        return INTERNAL_ERROR_MESSAGE.to_owned();
    }
    error.message().to_owned()
}

pub(crate) fn rpc_status(operation: &str, error: Error) -> Status {
    let message = rpc_error_message(operation, &error);
    match error.status() {
        Some(400) => Status::invalid_argument(message),
        Some(404) => Status::not_found(message),
        Some(501) => Status::unimplemented(message),
        _ => Status::unknown(format!("{operation}: {message}")),
    }
}

#[cfg(test)]
mod tests {
    use tonic::Code;

    use super::*;

    #[test]
    fn rpc_status_sanitizes_hidden_internal_errors() {
        let status = rpc_status("get cache entry", Error::hidden_internal("disk exploded"));
        assert_eq!(status.code(), Code::Unknown);
        assert_eq!(status.message(), "get cache entry: internal error");
    }

    #[test]
    fn rpc_status_preserves_explicit_errors() {
        let status = rpc_status("get cache entry", Error::bad_request("bad key"));
        assert_eq!(status.code(), Code::InvalidArgument);
        assert_eq!(status.message(), "bad key");
    }
}
