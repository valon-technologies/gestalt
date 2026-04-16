use std::fs;
use std::io::ErrorKind;
use std::path::{Path, PathBuf};
use std::process::Command;

const PROTO_FILES: &[&str] = &[
    "plugin.proto",
    "runtime.proto",
    "auth.proto",
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

    let mut prost_config = prost_build::Config::new();
    // Stable ordering makes the generated bindings and any derived snapshots easier to inspect.
    prost_config.btree_map(["."]);

    tonic_prost_build::configure()
        .out_dir(&out_dir)
        .build_client(true)
        .build_server(true)
        .compile_with_config(prost_config, &protos, &[proto_root])?;

    rustfmt(&out_dir.join("gestalt.provider.v1.rs"))?;

    Ok(())
}
