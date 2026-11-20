package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func mountedVolume(path string) (string, error) {
	data, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return "", err
	}
	best := ""
	unescape := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 6 {
			continue
		}
		mount := filepath.Clean(unescape.Replace(fields[4]))
		if (path == mount || strings.HasPrefix(path, strings.TrimSuffix(mount, "/")+"/")) && len(mount) > len(best) {
			best = mount
		}
	}
	if best == "" {
		return "", fmt.Errorf("no mount for %s", path)
	}
	return best, nil
}
