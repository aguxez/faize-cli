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
for cmd in sh mount mkdir cat ls chmod chown echo setsid grep; do
    ln -sf busybox "$WORK_DIR/rootfs/bin/$cmd"
done

# Copy busybox directly as /init (kernel may not follow symlinks during early init)
echo "==> Setting up init"
cp "$WORK_DIR/rootfs/bin/busybox" "$WORK_DIR/rootfs/init"
chmod +x "$WORK_DIR/rootfs/init"
mkdir -p "$WORK_DIR/rootfs/sbin"
ln -sf /bin/busybox "$WORK_DIR/rootfs/sbin/init"

# Create inittab for busybox init
cat > "$WORK_DIR/rootfs/etc/inittab" << 'INITTAB'
# Busybox inittab
::sysinit:/etc/init.d/rcS
::respawn:-/bin/sh
::ctrlaltdel:/sbin/reboot
INITTAB

# Create startup script
mkdir -p "$WORK_DIR/rootfs/etc/init.d"
cat > "$WORK_DIR/rootfs/etc/init.d/rcS" << 'STARTUP'
#!/bin/busybox sh
# Faize VM startup - bootstrap-aware

# Mount filesystems only if not already mounted
/bin/busybox grep -q "^proc " /proc/mounts 2>/dev/null || /bin/busybox mount -t proc proc /proc
/bin/busybox grep -q "^sysfs " /proc/mounts 2>/dev/null || /bin/busybox mount -t sysfs sys /sys
/bin/busybox grep -q "^devtmpfs " /proc/mounts 2>/dev/null || /bin/busybox mount -t devtmpfs dev /dev

/bin/busybox mkdir -p /mnt/bootstrap
if /bin/busybox mount -t virtiofs faize-bootstrap /mnt/bootstrap 2>/dev/null; then
    if [ -x /mnt/bootstrap/init.sh ]; then
        exec /mnt/bootstrap/init.sh
    fi
fi

echo "Faize: bootstrap mount failed or no init.sh found"
STARTUP

chmod +x "$WORK_DIR/rootfs/etc/init.d/rcS"

# Create ext4 filesystem image using truncate for sparse file (better macOS compatibility)
echo "==> Creating ext4 image (${ROOTFS_SIZE_MB}MB)"
truncate -s ${ROOTFS_SIZE_MB}M "$WORK_DIR/rootfs.img"

# Format and populate using Docker (works on both macOS and Linux)
echo "==> Formatting and populating rootfs"

# Create a tar of the rootfs
tar -C "$WORK_DIR/rootfs" -cf "$WORK_DIR/rootfs.tar" .

# Use Docker with privileged mode to mount and extract
# Note: sync and e2fsck are critical for macOS Virtualization.framework compatibility
docker run --rm --privileged \
    -v "$WORK_DIR:/work" \
    alpine:latest sh -c "
        apk add --no-cache e2fsprogs >/dev/null 2>&1
        mkfs.ext4 -F -L faize-root /work/rootfs.img >/dev/null
        mkdir -p /mnt/rootfs
        mount -o loop /work/rootfs.img /mnt/rootfs
        tar -C /mnt/rootfs -xf /work/rootfs.tar
        sync
        umount /mnt/rootfs
        sync
        # Verify and auto-fix filesystem for macOS compatibility
        e2fsck -f -y /work/rootfs.img >/dev/null 2>&1 || true
    "

# Move to final location
echo "==> Installing rootfs image"
mv "$WORK_DIR/rootfs.img" "$OUTPUT_PATH"

echo "==> Rootfs build complete!"
echo "    Location: $OUTPUT_PATH"
echo "    Size: ${ROOTFS_SIZE_MB}MB"
echo ""
echo "You can now use this rootfs with Faize VM."
