package config

import "golang.org/x/sys/unix"

func mountedVolume(path string) (string, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return "", err
	}
	return unix.ByteSliceToString(st.Mntonname[:]), nil
}
