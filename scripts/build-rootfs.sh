#!/bin/bash
set -euo pipefail

# Faize VM Rootfs Builder
# Creates a minimal Alpine-based rootfs with VirtioFS bootstrap support

OUTPUT_PATH="${1:-$HOME/.faize/artifacts/rootfs.img}"
WORK_DIR=$(mktemp -d)
ROOTFS_SIZE_MB=64

cleanup() {
    echo "Cleaning up..."
    rm -rf "$WORK_DIR"
}
trap cleanup EXIT

echo "==> Building Faize VM rootfs"
echo "    Output: $OUTPUT_PATH"
echo "    Work dir: $WORK_DIR"

# Ensure output directory exists
mkdir -p "$(dirname "$OUTPUT_PATH")"

# Create rootfs directory structure (no /lib needed for static busybox)
echo "==> Creating directory structure"
mkdir -p "$WORK_DIR/rootfs"/{bin,dev,etc,mnt/bootstrap,proc,sys,tmp}

# Extract STATIC busybox from Alpine using Docker
echo "==> Extracting statically-linked busybox from Alpine"
docker run --rm -v "$WORK_DIR/rootfs:/out" alpine:latest sh -c '
    # Install busybox-static package
    apk add --no-cache busybox-static >/dev/null 2>&1

    # Copy the statically linked busybox (no library dependencies)
    cp /bin/busybox.static /out/bin/busybox
    chmod +x /out/bin/busybox
'

# Create essential command symlinks
echo "==> Creating busybox symlinks"
for cmd in sh mount umount mkdir cat ls chmod chown echo setsid grep pivot_root; do
    ln -sf busybox "$WORK_DIR/rootfs/bin/$cmd"
done

# Create /init as a shell script that sets up ephemeral overlay
echo "==> Setting up init with ephemeral overlay"
cat > "$WORK_DIR/rootfs/init" << 'INITSCRIPT'
#!/bin/sh
# Faize VM init - ephemeral overlay root
# Stage 1: Set up overlay so all rootfs writes go to tmpfs (discarded on shutdown)

export PATH=/bin

# Mount essential virtual filesystems
/bin/mount -t proc proc /proc 2>/dev/null || true
/bin/mount -t sysfs sys /sys 2>/dev/null || true
/bin/mount -t devtmpfs dev /dev 2>/dev/null || true

# Set up ephemeral overlay (tmpfs-backed writable layer over read-only rootfs)
if /bin/grep -q overlay /proc/filesystems; then
    /bin/mount -t tmpfs -o size=512M tmpfs /tmp
    /bin/mkdir -p /tmp/overlay/upper /tmp/overlay/work /tmp/overlay/merged /tmp/overlay/lower
    /bin/mount --bind / /tmp/overlay/lower
    /bin/mount -t overlay overlay \
        -o lowerdir=/tmp/overlay/lower,upperdir=/tmp/overlay/upper,workdir=/tmp/overlay/work \
        /tmp/overlay/merged

    # Pivot into the overlay root
    cd /tmp/overlay/merged
    /bin/mkdir -p old_root
    pivot_root . old_root

    # Re-mount essentials in the new overlay root
    /bin/mount -t proc proc /proc 2>/dev/null || true
    /bin/mount -t sysfs sys /sys 2>/dev/null || true
    /bin/mount -t devtmpfs dev /dev 2>/dev/null || true

    # Detach old root (overlay keeps internal references to lower layer)
    /bin/umount -l /old_root 2>/dev/null || true
else
    echo "WARNING: overlayfs not available - rootfs is read-only, some operations may fail"
fi

# Stage 2: Mount bootstrap and hand off
/bin/mkdir -p /mnt/bootstrap
if /bin/mount -t virtiofs faize-bootstrap /mnt/bootstrap 2>/dev/null; then
    if [ -x /mnt/bootstrap/init.sh ]; then
        exec /mnt/bootstrap/init.sh
    fi
fi

echo "Faize: bootstrap mount failed or no init.sh found"
exec /bin/sh
INITSCRIPT
chmod +x "$WORK_DIR/rootfs/init"

# Create ext4 image INSIDE container, then extract with docker cp
# This bypasses Docker Desktop's unreliable bind mount sync on macOS
echo "==> Creating ext4 image (${ROOTFS_SIZE_MB}MB)"
CONTAINER_ID=$(docker create \
    -v "$WORK_DIR/rootfs:/work/rootfs:ro" \
    alpine:latest sh -c "
        apk add --no-cache e2fsprogs >/dev/null 2>&1
        mke2fs -t ext4 -d /work/rootfs -L faize-root \
            -E no_copy_xattrs -b 4096 /tmp/rootfs.img ${ROOTFS_SIZE_MB}M
        e2fsck -f -y /tmp/rootfs.img >/dev/null 2>&1 || true
    ")

if ! docker start -a "$CONTAINER_ID"; then
    echo "ERROR: Failed to create ext4 image inside container"
    docker logs "$CONTAINER_ID" 2>&1 || true
    docker rm "$CONTAINER_ID" >/dev/null 2>&1 || true
    exit 1
fi

docker cp "$CONTAINER_ID:/tmp/rootfs.img" "$OUTPUT_PATH"
docker rm "$CONTAINER_ID" >/dev/null 2>&1 || true

echo "==> Rootfs build complete!"
echo "    Location: $OUTPUT_PATH"
echo "    Size: ${ROOTFS_SIZE_MB}MB"
echo ""
echo "You can now use this rootfs with Faize VM."
