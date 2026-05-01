/// Generated protobuf and gRPC bindings compiled from `sdk/proto/v1/*.proto`.
#[allow(clippy::all)]
#[allow(dead_code)]
pub mod v1 {
    pub use super::gestalt::provider::v1::*;
}

#[allow(clippy::all)]
#[allow(dead_code)]
pub mod gestalt {
    pub mod provider {
        pub mod v1 {
            include!("generated/gestalt/provider/v1/gestalt.provider.v1.rs");
        }
    }
}

#[allow(clippy::all)]
#[allow(dead_code)]
pub mod google {
    pub mod rpc {
        include!("generated/google/rpc/google.rpc.rs");
    }
}
