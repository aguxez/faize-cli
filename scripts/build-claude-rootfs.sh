#!/bin/bash
set -euo pipefail

# Faize Claude VM Rootfs Builder
# Creates an Alpine-based rootfs with development tools for Claude Code

OUTPUT_PATH="${1:-$HOME/.faize/artifacts/claude-rootfs.img}"
WORK_DIR=$(mktemp -d)
ROOTFS_SIZE_MB=1024

cleanup() {
    echo "Cleaning up..."
    rm -rf "$WORK_DIR"
}
trap cleanup EXIT

echo "==> Building Faize Claude VM rootfs"
echo "    Output: $OUTPUT_PATH"
echo "    Work dir: $WORK_DIR"

# Ensure output directory exists
mkdir -p "$(dirname "$OUTPUT_PATH")"

# Create rootfs directory structure
echo "==> Creating directory structure"
mkdir -p "$WORK_DIR/rootfs"/{bin,dev,etc,mnt/bootstrap,mnt/host-claude,opt/toolchain,proc,root,sys,tmp,workspace,usr/local/bin,usr/local/lib}

# set permissions for tmp directory
chmod 1777 "$WORK_DIR/rootfs/tmp"

# Extra dependencies passed via environment variable (space-separated)
EXTRA_DEPS="${EXTRA_DEPS:-}"

# Extract packages from Alpine using Docker
echo "==> Installing packages from Alpine"
if [ -n "$EXTRA_DEPS" ]; then
    echo "    Extra packages: $EXTRA_DEPS"
fi
docker run --rm -v "$WORK_DIR/rootfs:/out" alpine:latest sh -c "
    # Install packages
    BASE_PKGS=\"bash curl ca-certificates git build-base python3 coreutils nodejs npm util-linux iptables ip6tables\"
    apk add --no-cache \$BASE_PKGS $EXTRA_DEPS >/dev/null 2>&1

    # Copy the entire root filesystem structure
    for dir in bin lib usr sbin; do
        cp -a /\$dir /out/ 2>/dev/null || true
    done

    # Copy etc but be selective
    cp -a /etc /out/

    # Ensure busybox is available as fallback
    cp /bin/busybox /out/bin/busybox 2>/dev/null || true

    # Create non-root claude user for running Claude CLI
    adduser -D -h /home/claude -s /bin/sh claude
    mkdir -p /out/home/claude/.claude

    # Copy passwd/group/shadow to rootfs for user to exist
    cp /etc/passwd /out/etc/passwd
    cp /etc/group /out/etc/group
    cp /etc/shadow /out/etc/shadow
"

# Install Claude Code CLI only (Node.js already in rootfs)
echo "==> Installing Claude Code CLI"
docker run --rm -v "$WORK_DIR/rootfs:/out" alpine:latest sh -c '
    # Install Node.js and npm in this container to run npm install
    apk add --no-cache nodejs npm >/dev/null 2>&1

    # Install Claude Code CLI globally
    echo "Installing Claude Code CLI..."
    npm install -g @anthropic-ai/claude-code || { echo "npm install failed"; exit 1; }

    # Verify installation
    if [ ! -x /usr/local/bin/claude ]; then
        echo "Error: claude CLI not found after npm install"
        exit 1
    fi

    # Copy only the Claude CLI files to rootfs (Node.js is already there from step 1)
    cp -a /usr/local/bin/claude /out/usr/local/bin/claude
    mkdir -p /out/usr/local/lib/node_modules
    cp -a /usr/local/lib/node_modules/@anthropic-ai /out/usr/local/lib/node_modules/

    echo "Claude CLI installed successfully"
'

echo "==> Creating init script (ephemeral overlay)"
cat > "$WORK_DIR/rootfs/init" << 'INITSCRIPT'
#!/bin/sh
# Faize Claude VM init - ephemeral overlay root
# Stage 1: Set up overlay so all rootfs writes go to tmpfs (discarded on shutdown)

export PATH=/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin

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

# Fallback to shell
echo "Faize Claude: bootstrap mount failed or no init.sh found"
exec /bin/sh
INITSCRIPT
chmod +x "$WORK_DIR/rootfs/init"

# Create env.sh template for toolchain
cat > "$WORK_DIR/rootfs/opt/toolchain/env.sh" << 'ENVSH'
#!/bin/bash
# Toolchain environment (populated by first boot)
export PATH=/opt/toolchain/bin:/usr/local/bin:$PATH
ENVSH
chmod +x "$WORK_DIR/rootfs/opt/toolchain/env.sh"

# Create ext4 image INSIDE container, then extract with docker cp
# This bypasses Docker Desktop's unreliable bind mount sync on macOS
echo "==> Creating ext4 image (${ROOTFS_SIZE_MB}MB)"
CONTAINER_ID=$(docker create \
    -v "$WORK_DIR/rootfs:/work/rootfs:ro" \
    alpine:latest sh -c "
        apk add --no-cache e2fsprogs >/dev/null 2>&1
        mke2fs -t ext4 -d /work/rootfs -L faize-claude \
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

echo "==> Rootfs image created at $OUTPUT_PATH"

echo "==> Claude rootfs build complete!"
echo "    Location: $OUTPUT_PATH"
echo "    Size: ${ROOTFS_SIZE_MB}MB"
