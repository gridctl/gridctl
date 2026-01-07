package builder

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
)

// CacheDir returns the agentlab cache directory.
func CacheDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	cacheDir := filepath.Join(home, ".agentlab", "cache")
	return cacheDir, nil
}

// ReposCacheDir returns the directory for cached git repositories.
func ReposCacheDir() (string, error) {
	cacheDir, err := CacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cacheDir, "repos"), nil
}

// URLToPath converts a git URL to a cache path using a hash.
func URLToPath(url string) (string, error) {
	reposDir, err := ReposCacheDir()
	if err != nil {
		return "", err
	}

	// Create a hash of the URL for the directory name
	hash := sha256.Sum256([]byte(url))
	hashStr := hex.EncodeToString(hash[:8]) // Use first 8 bytes

	return filepath.Join(reposDir, hashStr), nil
}

// EnsureCacheDir creates the cache directory if it doesn't exist.
func EnsureCacheDir() error {
	cacheDir, err := CacheDir()
	if err != nil {
		return err
	}
	return os.MkdirAll(cacheDir, 0755)
}

// EnsureReposCacheDir creates the repos cache directory if it doesn't exist.
func EnsureReposCacheDir() error {
	reposDir, err := ReposCacheDir()
	if err != nil {
		return err
	}
	return os.MkdirAll(reposDir, 0755)
}

// CleanCache removes all cached data.
func CleanCache() error {
	cacheDir, err := CacheDir()
	if err != nil {
		return err
	}
	return os.RemoveAll(cacheDir)
}
