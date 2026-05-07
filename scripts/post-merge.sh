#!/bin/bash
set -e

echo "==> Building kaptaan binary..."
GONOSUMDB='*' GOFLAGS='-mod=mod' /nix/store/a90l6nxkqdlqxzgz5j958rz5gwygbamc-go-1.21.13/bin/go build -o kaptaan ./cmd/kaptaan

echo "==> Build complete."
