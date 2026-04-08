use tonic::Status;

use crate::Error;

pub(crate) fn rpc_status(operation: &str, error: Error) -> Status {
    match error.status() {
        Some(status) if status == http::StatusCode::BAD_REQUEST.as_u16() => {
            Status::invalid_argument(error.message().to_owned())
        }
        Some(status) if status == http::StatusCode::NOT_FOUND.as_u16() => {
            Status::not_found(error.message().to_owned())
        }
        Some(status) if status == http::StatusCode::NOT_IMPLEMENTED.as_u16() => {
            Status::unimplemented(error.message().to_owned())
        }
        _ => Status::unknown(format!("{operation}: {}", error.message())),
    }
}
