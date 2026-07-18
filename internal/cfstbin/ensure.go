package cfstbin

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	BinaryName = "CloudflareST"
	IPFileName = "ip.txt"
)

// Ensure extracts the bundled CloudflareST binary and helper files into dataDir
// when they are missing. Returns the absolute binary path.
func Ensure(dataDir string) (binaryPath string, err error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return "", err
	}

	archDir, err := assetDir()
	if err != nil {
		return "", err
	}

	binaryPath = filepath.Join(dataDir, BinaryName)
	if needsWrite(binaryPath) {
		if err := writeAsset(path.Join(archDir, BinaryName), binaryPath, 0o755); err != nil {
			return "", fmt.Errorf("extract %s: %w", BinaryName, err)
		}
	} else {
		// Ensure executable bit on Unix-like systems.
		_ = os.Chmod(binaryPath, 0o755)
	}

	// Helper files: only create when missing, never overwrite user edits.
	helpers := []string{IPFileName, "ipv6.txt"}
	for _, name := range helpers {
		dst := filepath.Join(dataDir, name)
		if !needsWrite(dst) {
			continue
		}
		src := path.Join(archDir, name)
		if _, err := FS.Open(src); err != nil {
			continue
		}
		if err := writeAsset(src, dst, 0o644); err != nil {
			return "", fmt.Errorf("extract %s: %w", name, err)
		}
	}

	return binaryPath, nil
}

func assetDir() (string, error) {
	switch runtime.GOOS {
	case "linux":
		switch runtime.GOARCH {
		case "amd64":
			return "linux_amd64", nil
		case "arm64":
			return "linux_arm64", nil
		default:
			return "", fmt.Errorf("bundled CloudflareST does not support linux/%s yet", runtime.GOARCH)
		}
	default:
		// For non-linux hosts we still extract linux_amd64 assets for packaging checks,
		// but runtime speed tests are expected on Linux servers.
		if runtime.GOARCH == "arm64" {
			return "linux_arm64", nil
		}
		return "linux_amd64", nil
	}
}

func needsWrite(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return true
	}
	return info.IsDir() || info.Size() == 0
}

func writeAsset(src, dst string, mode fs.FileMode) error {
	data, err := FS.ReadFile(src)
	if err != nil {
		return err
	}
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(dst)
		if err2 := os.Rename(tmp, dst); err2 != nil {
			return err
		}
	}
	return os.Chmod(dst, mode)
}

func SupportsCurrentPlatform() bool {
	return runtime.GOOS == "linux" && (runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64")
}

func PlatformLabel() string {
	return strings.ToLower(runtime.GOOS + "/" + runtime.GOARCH)
}
