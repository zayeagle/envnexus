package command

import (
	"encoding/json"
	"strings"

	"github.com/zayeagle/envnexus/libs/shared/pkg/agentprompt"
	"github.com/zayeagle/envnexus/services/platform-api/internal/domain"
)

// NLTargetFromDevice builds the NL command generation snapshot from enrollment + last heartbeat metadata.
func NLTargetFromDevice(d *domain.Device) agentprompt.Snapshot {
	if d == nil {
		return agentprompt.DefaultNLTargetWhenNoDevice()
	}
	s := agentprompt.Snapshot{
		OS:         strings.TrimSpace(d.Platform),
		Arch:       strings.TrimSpace(d.Arch),
		DeviceName: strings.TrimSpace(d.DeviceName),
	}
	if d.Hostname != nil {
		s.Hostname = strings.TrimSpace(*d.Hostname)
	}
	if d.RuntimeMetadata != nil && strings.TrimSpace(*d.RuntimeMetadata) != "" {
		var meta struct {
			OSVersion string `json:"os_version"`
			Shell     string `json:"shell"`
			WorkDir   string `json:"work_dir"`
			User      string `json:"user"`
		}
		if err := json.Unmarshal([]byte(*d.RuntimeMetadata), &meta); err == nil {
			s.OSVersion = meta.OSVersion
			s.Shell = meta.Shell
			s.WorkDir = meta.WorkDir
			if meta.User != "" {
				s.UserProfile = meta.User
			}
		}
	}
	return agentprompt.NormalizeSnapshot(s)
}
