use std::path::PathBuf;

fn main() {
    let protoc = protoc_bin_vendored::protoc_bin_path().expect("resolve vendored protoc");
    // SAFETY: the build script runs in a short-lived Cargo-managed process and sets
    // PROTOC before prost/tonic invoke protoc within this same process.
    unsafe {
        std::env::set_var("PROTOC", protoc);
    }

    let manifest_dir = PathBuf::from(std::env::var("CARGO_MANIFEST_DIR").expect("manifest dir"));
    let proto_root = manifest_dir.join("..").join("proto");
    let protos = [
        proto_root.join("v1").join("plugin.proto"),
        proto_root.join("v1").join("runtime.proto"),
        proto_root.join("v1").join("auth.proto"),
        proto_root.join("v1").join("datastore.proto"),
    ];
    let includes = [proto_root];
    let mut prost_config = prost_build::Config::new();

    // Stable ordering makes the generated bindings and any derived snapshots easier to inspect.
    prost_config.btree_map(["."]);

    tonic_prost_build::configure()
        .build_client(true)
        .build_server(true)
        .compile_with_config(prost_config, &protos, &includes)
        .expect("compile protobuf bindings");
}
