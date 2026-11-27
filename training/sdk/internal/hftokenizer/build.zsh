#!/bin/zsh
# Rebuild the embedded WASI module with the toolchain and crate versions pinned here.
emulate -LR zsh
set -euo pipefail
cd ${0:A:h}
if [[ "$(rustc --version)" != "rustc 1.94.0 "* ]]; then
    print -u2 'Use rustup toolchain 1.94.0 with the wasm32-wasip1 target.'
    exit 1
fi
export CARGO_TARGET_DIR=${CARGO_TARGET_DIR:-$PWD/target}
export CARGO_TARGET_WASM32_WASIP1_RUSTFLAGS="${CARGO_TARGET_WASM32_WASIP1_RUSTFLAGS:-} --remap-path-prefix=$PWD=/guest --remap-path-prefix=${CARGO_HOME:-$HOME/.cargo}/registry=/cargo/registry"
cargo build --release --locked --target wasm32-wasip1
install -m 0644 "$CARGO_TARGET_DIR/wasm32-wasip1/release/fireworks_tokenizer.wasm" tokenizer.wasm
