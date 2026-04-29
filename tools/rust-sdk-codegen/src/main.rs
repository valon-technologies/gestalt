use std::fs;
use std::io::ErrorKind;
use std::path::{Path, PathBuf};
use std::process::Command;

const GOOGLE_RPC_STATUS_PROTO: &str = r#"syntax = "proto3";

package google.rpc;

import "google/protobuf/any.proto";

message Status {
  int32 code = 1;
  string message = 2;
  repeated google.protobuf.Any details = 3;
}
"#;

const PROTO_FILES: &[&str] = &[
    "agent.proto",
    "authentication.proto",
    "plugin.proto",
    "runtime.proto",
    "cache.proto",
    "secrets.proto",
    "datastore.proto",
    "s3.proto",
    "workflow.proto",
];

fn repo_root() -> PathBuf {
    PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .ancestors()
        .nth(2)
        .expect("repo root")
        .to_path_buf()
}

fn rustfmt(path: &Path) -> Result<(), Box<dyn std::error::Error>> {
    match Command::new("rustfmt")
        .arg("--edition")
        .arg("2024")
        .arg(path)
        .status()
    {
        Ok(status) if status.success() => Ok(()),
        Ok(status) => Err(format!("rustfmt failed for {}: {status}", path.display()).into()),
        Err(error) if error.kind() == ErrorKind::NotFound => Ok(()),
        Err(error) => Err(error.into()),
    }
}

fn google_rpc_status_proto(
    repo_root: &Path,
) -> Result<(PathBuf, PathBuf), Box<dyn std::error::Error>> {
    if let Ok(googleapis_dir) = std::env::var("GOOGLEAPIS_PROTO_DIR") {
        let googleapis_dir = PathBuf::from(googleapis_dir);
        let status_proto = googleapis_dir
            .join("google")
            .join("rpc")
            .join("status.proto");
        return Ok((googleapis_dir, status_proto));
    }

    let googleapis_dir = repo_root
        .join("sdk")
        .join("rust")
        .join("target")
        .join("codegen-googleapis");
    let status_proto = googleapis_dir
        .join("google")
        .join("rpc")
        .join("status.proto");
    fs::create_dir_all(status_proto.parent().expect("status proto parent"))?;
    fs::write(&status_proto, GOOGLE_RPC_STATUS_PROTO)?;
    Ok((googleapis_dir, status_proto))
}

fn main() -> Result<(), Box<dyn std::error::Error>> {
    let protoc = protoc_bin_vendored::protoc_bin_path()?;
    // SAFETY: the codegen helper runs as a short-lived standalone process and sets
    // PROTOC before prost/tonic invoke protoc within this same process.
    unsafe {
        std::env::set_var("PROTOC", protoc);
    }

    let repo_root = repo_root();
    let proto_root = repo_root.join("sdk").join("proto");
    let out_dir = repo_root
        .join("sdk")
        .join("rust")
        .join("src")
        .join("generated");
    fs::create_dir_all(&out_dir)?;

    let protos: Vec<_> = PROTO_FILES
        .iter()
        .map(|name| proto_root.join("v1").join(name))
        .collect();
    let mut protos = protos;
    let mut includes = vec![proto_root.clone()];
    let (googleapis_dir, status_proto) = google_rpc_status_proto(&repo_root)?;
    protos.push(status_proto);
    includes.push(googleapis_dir);

    let mut prost_config = prost_build::Config::new();
    // Stable ordering makes the generated bindings and any derived snapshots easier to inspect.
    prost_config.btree_map(["."]);
    prost_config.extern_path(".google.rpc", "crate::generated::google::rpc");

    tonic_prost_build::configure()
        .out_dir(&out_dir)
        .build_client(true)
        .build_server(true)
        .compile_with_config(prost_config, &protos, &includes)?;

    rustfmt(&out_dir.join("gestalt.provider.v1.rs"))?;
    let google_rpc = out_dir.join("google.rpc.rs");
    if google_rpc.exists() {
        rustfmt(&google_rpc)?;
    }

    Ok(())
}
