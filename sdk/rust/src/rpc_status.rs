use tonic::Status;

use crate::Error;

pub(crate) fn rpc_status(operation: &str, error: Error) -> Status {
    match error.status() {
        Some(400) => Status::invalid_argument(error.message().to_owned()),
        Some(404) => Status::not_found(error.message().to_owned()),
        Some(501) => Status::unimplemented(error.message().to_owned()),
        _ => Status::unknown(format!("{operation}: {}", error.message())),
    }
}
