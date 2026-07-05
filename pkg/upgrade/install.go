package upgrade

import (
	"fmt"
	"os"
	"path/filepath"
)

// FileInstaller writes upgraded binaries to disk.
type FileInstaller struct{}

// Install replaces the target binary with new contents and keeps a .bak backup.
func (FileInstaller) Install(installPath string, data []byte) error {
	if err := ensureWritable(filepath.Dir(installPath), installPath); err != nil {
		return err
	}

	backupPath := installPath + ".bak"
	if _, err := os.Stat(installPath); err == nil {
		if err := copyFile(installPath, backupPath); err != nil {
			return fmt.Errorf("backup current binary: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect install path: %w", err)
	}

	tempPath := installPath + ".new"
	if err := os.WriteFile(tempPath, data, 0755); err != nil {
		return fmt.Errorf("write new binary: %w", err)
	}
	if err := os.Rename(tempPath, installPath); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("install new binary: %w", err)
	}
	return nil
}

// RestoreBackup restores the .bak backup over the install path.
func (FileInstaller) RestoreBackup(installPath string) error {
	backupPath := installPath + ".bak"
	if _, err := os.Stat(backupPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("inspect backup binary: %w", err)
	}
	if err := os.Rename(backupPath, installPath); err != nil {
		return fmt.Errorf("restore backup binary: %w", err)
	}
	return nil
}

func ensureWritable(dir, installPath string) error {
	testPath := filepath.Join(dir, ".warpgate-upgrade-test")
	file, err := os.OpenFile(testPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		if os.IsPermission(err) {
			return fmt.Errorf("install path %s is not writable; rerun with sudo", installPath)
		}
		return fmt.Errorf("check install path permissions: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close install path permission test file: %w", err)
	}
	if err := os.Remove(testPath); err != nil {
		return fmt.Errorf("remove install path permission test file: %w", err)
	}
	return nil
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.WriteFile(dst, data, 0755); err != nil {
		return err
	}
	return nil
}
