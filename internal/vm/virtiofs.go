//go:build darwin

package vm

import (
	"fmt"

	"github.com/Code-Hex/vz/v3"
	"github.com/faize-ai/faize/internal/session"
)

// createVirtioFSDevices creates VirtioFS device configurations from mounts
func createVirtioFSDevices(mounts []session.VMMount) ([]vz.DirectorySharingDeviceConfiguration, error) {
	var devices []vz.DirectorySharingDeviceConfiguration

	for i, mount := range mounts {
		// Generate tag if not set
		tag := mount.Tag
		if tag == "" {
			tag = fmt.Sprintf("mount%d", i)
		}

		// Create shared directory
		share, err := vz.NewSharedDirectory(mount.Source, mount.ReadOnly)
		if err != nil {
			return nil, fmt.Errorf("failed to create shared directory for %s: %w", mount.Source, err)
		}

		// Create single directory share
		single, err := vz.NewSingleDirectoryShare(share)
		if err != nil {
			return nil, fmt.Errorf("failed to create directory share for %s: %w", mount.Source, err)
		}

		// Create VirtioFS device configuration
		device, err := vz.NewVirtioFileSystemDeviceConfiguration(tag)
		if err != nil {
			return nil, fmt.Errorf("failed to create VirtioFS device for %s: %w", mount.Source, err)
		}
		device.SetDirectoryShare(single)

		devices = append(devices, device)
	}

	return devices, nil
}

// GenerateMountScript generates a shell script to mount VirtioFS shares in the guest
func GenerateMountScript(mounts []session.VMMount) string {
	script := "#!/bin/sh\nset -e\n\n"

	for i, mount := range mounts {
		tag := mount.Tag
		if tag == "" {
			tag = fmt.Sprintf("mount%d", i)
		}

		// Create mount point directory
		script += fmt.Sprintf("mkdir -p %s\n", mount.Target)

		// Mount with appropriate options
		opts := "rw"
		if mount.ReadOnly {
			opts = "ro"
		}
		script += fmt.Sprintf("mount -t virtiofs %s %s -o %s\n", tag, mount.Target, opts)
	}

	return script
}
