// Links the Rust client against the C ABI shared library built by `make shared`.
//
// The library lives in <repo>/build. We link against it dynamically and embed an
// rpath so binaries and test executables can find it without environment
// variables. Windows is not supported yet.

use std::path::PathBuf;

fn main() {
    let manifest = PathBuf::from(
        std::env::var("CARGO_MANIFEST_DIR").expect("CARGO_MANIFEST_DIR is always set by Cargo"),
    );
    let root = manifest
        .parent()
        .and_then(|p| p.parent())
        .expect("clients/rust should live inside the repository");
    let build = match std::env::var_os("YOUSDB_BUILD_DIR") {
        Some(dir) => PathBuf::from(dir),
        None => root.join("build"),
    };

    let library = if cfg!(target_os = "macos") {
        build.join("libyousdb.dylib")
    } else if cfg!(target_os = "windows") {
        build.join("yousdb.dll")
    } else {
        build.join("libyousdb.so")
    };

    if !library.exists() {
        panic!(
            "{} is missing; run `make shared` from the repository root first",
            library.display()
        );
    }

    println!("cargo:rustc-link-search=native={}", build.display());
    println!("cargo:rustc-link-lib=dylib=yousdb");
    println!("cargo:rustc-link-arg=-Wl,-rpath,{}", build.display());
    println!("cargo:rerun-if-changed={}", library.display());
    println!("cargo:rerun-if-env-changed=YOUSDB_BUILD_DIR");
}
