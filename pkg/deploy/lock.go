package deploy

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/pangobit/warpgate/pkg/ssh"
	"github.com/sirupsen/logrus"
)

const lockTimeout = 30 * time.Minute

// LockInfo contains metadata about who holds a deploy lock.
type LockInfo struct {
	// Host is the hostname of the machine that acquired the lock.
	Host string `json:"host"`
	// User is the username that acquired the lock.
	User string `json:"user"`
	// AcquiredAt is when the lock was acquired.
	AcquiredAt time.Time `json:"acquired_at"`
}

func acquireLock(client *ssh.Client, appDir string, log *logrus.Logger) error {
	lockDir := appDir + "/.lock"

	_, stderr, err := client.RunCommand("mkdir " + lockDir + " 2>&1")
	if err != nil {
		info, readErr := readLockInfo(client, lockDir)
		if readErr != nil || info == nil {
			return fmt.Errorf("deploy lock held (could not read lock info: %s)", strings.TrimSpace(stderr))
		}
		age := time.Since(info.AcquiredAt).Truncate(time.Second)
		if age > lockTimeout {
			log.Warnf("Breaking stale lock held by %s@%s since %s (%s ago)",
				info.User, info.Host, info.AcquiredAt.Format(time.RFC3339), age)
			if _, breakErr := breakLock(client, appDir, log); breakErr != nil {
				return fmt.Errorf("failed to break stale lock: %w", breakErr)
			}
			_, stderr, err = client.RunCommand("mkdir " + lockDir + " 2>&1")
			if err != nil {
				return fmt.Errorf("failed to acquire lock after breaking stale lock: %s", strings.TrimSpace(stderr))
			}
		} else {
			return fmt.Errorf("deploy lock held by %s@%s since %s (%s ago)",
				info.User, info.Host, info.AcquiredAt.Format(time.RFC3339), age)
		}
	}

	info := LockInfo{
		User:       currentUser(),
		AcquiredAt: time.Now(),
	}
	host, err := os.Hostname()
	if err == nil {
		info.Host = host
	}

	if data, err := json.Marshal(info); err == nil {
		if writeErr := client.WriteFile(lockDir+"/info", string(data)); writeErr != nil {
			releaseLock(client, appDir, log)
			return fmt.Errorf("failed to write lock info: %w", writeErr)
		}
	}

	return nil
}

func releaseLock(client *ssh.Client, appDir string, log *logrus.Logger) {
	lockDir := appDir + "/.lock"
	if _, _, err := client.RunCommand("rm -rf " + lockDir); err != nil {
		log.Warnf("Failed to release deploy lock at %s: %v", lockDir, err)
	}
}

func breakLock(client *ssh.Client, appDir string, log *logrus.Logger) (*LockInfo, error) {
	lockDir := appDir + "/.lock"
	info, err := readLockInfo(client, lockDir)
	if err != nil {
		log.Warnf("Could not read lock info at %s: %v", lockDir, err)
	}
	_, _, err = client.RunCommand("rm -rf " + lockDir)
	if err != nil {
		return info, fmt.Errorf("failed to break lock: %w", err)
	}
	return info, nil
}

func readLockInfo(client *ssh.Client, lockDir string) (*LockInfo, error) {
	stdout, _, err := client.RunCommand("cat " + lockDir + "/info 2>/dev/null")
	if err != nil {
		return nil, err
	}
	stdout = strings.TrimSpace(stdout)
	if stdout == "" {
		return nil, fmt.Errorf("empty lock info")
	}
	var info LockInfo
	if err := json.Unmarshal([]byte(stdout), &info); err != nil {
		return nil, err
	}
	return &info, nil
}

func currentUser() string {
	user := os.Getenv("USER")
	if user == "" {
		user = "unknown"
	}
	return user
}
