#!/bin/bash
# Build uncompressed ARM64 Linux kernel for Apple Virtualization.framework
# CRITICAL: Apple Silicon requires ARM64 Image format (not ELF vmlinux)

set -euo pipefail

VERSION="${1:-6.6.10}"
WORKDIR="${2:-$(mktemp -d)}"
OUTPUT="${3:-./vmlinux}"

echo "Building Linux kernel $VERSION..."
echo "Work directory: $WORKDIR"

# Get the directory where this script is located
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
KERNEL_CONFIG="${SCRIPT_DIR}/kernel-config-arm64-minimal"

# Verify kernel config exists
if [[ ! -f "$KERNEL_CONFIG" ]]; then
    echo "ERROR: Kernel config not found at $KERNEL_CONFIG"
    exit 1
fi

# Shared build function (can be sourced or executed inline)
build_kernel() {
    local VERSION="$1"
    local WORKDIR="$2"
    local OUTPUT="$3"
    local CROSS_COMPILE="$4"
    local KERNEL_CONFIG="$5"

    cd "$WORKDIR"

    # Download kernel source
    if [ ! -f "linux-${VERSION}.tar.xz" ]; then
        echo "Downloading kernel source..."
        curl -LO "https://cdn.kernel.org/pub/linux/kernel/v6.x/linux-${VERSION}.tar.xz"
    fi

    # Extract
    if [ ! -d "linux-${VERSION}" ]; then
        echo "Extracting..."
        tar xf "linux-${VERSION}.tar.xz"
    fi

    cd "linux-${VERSION}"

    # Copy kernel config
    cat "$KERNEL_CONFIG" > .config

    echo "Configuring kernel..."
    make ARCH=arm64 CROSS_COMPILE="${CROSS_COMPILE}" olddefconfig

    echo "Building kernel (this may take a while)..."
    make ARCH=arm64 CROSS_COMPILE="${CROSS_COMPILE}" -j$(nproc) Image

    echo "Copying output..."
    cp arch/arm64/boot/Image "$OUTPUT"

    echo "Kernel built successfully!"
}

# Export function for Docker subshell
export -f build_kernel

# On macOS, use Docker for cross-compilation
if [[ "$(uname)" == "Darwin" ]]; then
    echo "Detected macOS - using Docker for cross-compilation..."

    if ! command -v docker &> /dev/null; then
        echo "ERROR: Docker is required on macOS for kernel cross-compilation."
        echo "Install Docker Desktop from: https://www.docker.com/products/docker-desktop"
        exit 1
    fi

    # Check if Docker daemon is running
    if ! docker info &> /dev/null; then
        echo "ERROR: Docker daemon is not running."
        echo "Please start Docker Desktop and try again."
        exit 1
    fi

    # Resolve output to absolute path
    OUTPUT_DIR="$(cd "$(dirname "$OUTPUT")" && pwd)"
    OUTPUT_NAME="$(basename "$OUTPUT")"
    OUTPUT_ABS="${OUTPUT_DIR}/${OUTPUT_NAME}"

    echo "Building kernel in Docker container..."
    echo "Output will be: $OUTPUT_ABS"

    # Run build inside Docker container (debian-slim is ~30MB vs ubuntu's ~78MB)
    # Note: --ulimit nofile increases file descriptor limit to avoid "Too many open files" during parallel build
    docker run --rm \
        --platform linux/arm64 \
        --ulimit nofile=65536:65536 \
        -v "${WORKDIR}:/build" \
        -v "${OUTPUT_DIR}:/output" \
        -v "${KERNEL_CONFIG}:/kernel-config:ro" \
        -e VERSION="$VERSION" \
        -e OUTPUT_NAME="$OUTPUT_NAME" \
        debian:bookworm-slim bash -c '
set -euo pipefail

echo "Installing build dependencies..."
apt-get update -qq
apt-get install -y -qq gcc-aarch64-linux-gnu make flex bison bc libssl-dev libelf-dev curl xz-utils >/dev/null

cd /build

# Download kernel source
if [ ! -f "linux-${VERSION}.tar.xz" ]; then
    echo "Downloading kernel source..."
    curl -LO "https://cdn.kernel.org/pub/linux/kernel/v6.x/linux-${VERSION}.tar.xz"
fi

# Extract
if [ ! -d "linux-${VERSION}" ]; then
    echo "Extracting..."
    tar xf "linux-${VERSION}.tar.xz"
fi

cd "linux-${VERSION}"

# Copy kernel config
cat /kernel-config > .config

echo "Configuring kernel..."
make ARCH=arm64 CROSS_COMPILE=aarch64-linux-gnu- olddefconfig

echo "Building kernel (this may take a while)..."
# Use limited parallelism to avoid "Too many open files" in Docker on macOS
make ARCH=arm64 CROSS_COMPILE=aarch64-linux-gnu- -j4 Image

echo "Copying output..."
cp arch/arm64/boot/Image "/output/${OUTPUT_NAME}"

echo "Kernel built successfully!"
'

    echo "Kernel built: $OUTPUT_ABS"
    echo "Size: $(du -h "$OUTPUT_ABS" | cut -f1)"
    exit 0
fi

# Linux: build directly with cross-compiler
echo "Detected Linux - building with cross-compiler..."
build_kernel "$VERSION" "$WORKDIR" "$OUTPUT" "aarch64-linux-gnu-" "$KERNEL_CONFIG"

echo "Kernel built: $OUTPUT"
echo "Size: $(du -h "$OUTPUT" | cut -f1)"
