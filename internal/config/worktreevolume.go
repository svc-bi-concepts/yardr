package config

import (
	"os"
	"path/filepath"
)

func WorktreeVolume(path string) (string, error) {
	path = filepath.Clean(path)
	for {
		resolved, err := filepath.EvalSymlinks(path)
		if err == nil {
			return mountedVolume(resolved)
		}
		if !os.IsNotExist(err) || path == filepath.Dir(path) {
			return "", err
		}
		path = filepath.Dir(path)
	}
}

func WorktreeMounted(path, volume string) bool {
	fi, err := os.Stat(path)
	if err != nil || !fi.IsDir() {
		return false
	}
	if volume == "" {
		return false
	}
	actual, err := WorktreeVolume(path)
	return err == nil && actual == filepath.Clean(volume)
}
