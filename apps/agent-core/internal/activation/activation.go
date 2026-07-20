package activation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/zayeagle/envnexus/apps/agent-core/internal/hwinfo"
)

type Status struct {
	Activated      bool   `json:"activated"`
	DeviceCode     string `json:"device_code"`
	PackageID      string `json:"package_id,omitempty"`
	TenantID       string `json:"tenant_id,omitempty"`
	ActivationMode string `json:"activation_mode"`
}

type Manager struct {
	platformURL    string
	activationMode string
	activationKey  string
	configDir      string
	status         *Status
}

func NewManager(platformURL, activationMode, activationKey, configDir string) *Manager {
	m := &Manager{
		platformURL:    platformURL,
		activationMode: activationMode,
		activationKey:  activationKey,
		configDir:      configDir,
	}
	m.loadStatus()
	return m
}

func (m *Manager) IsActivated() bool {
	return m.status != nil && m.status.Activated
}

func (m *Manager) GetStatus() *Status {
	if m.status == nil {
		return &Status{Activated: false, ActivationMode: m.activationMode}
	}
	return m.status
}

func (m *Manager) GetDeviceCode() string {
	if m.status != nil {
		return m.status.DeviceCode
	}
	return ""
}

func (m *Manager) Activate(ctx context.Context) error {
	if m.IsActivated() {
		return nil
	}

	components := hwinfo.CollectComponents()
	if len(components) == 0 {
		return fmt.Errorf("failed to collect hardware components")
	}

	compInfos := make([]map[string]string, 0, len(components))
	for _, c := range components {
		compInfos = append(compInfos, map[string]string{"type": c.Type, "hash": c.Hash})
	}

	hwHash := hwinfo.CompositeHash(components)
	deviceCode := generateDeviceCode(hwHash)

	// Persist device code early so the dashboard can display it even if activation fails
	if m.status == nil || m.status.DeviceCode == "" {
		m.status = &Status{
			Activated:      false,
			DeviceCode:     deviceCode,
			ActivationMode: m.activationMode,
		}
		m.saveStatus()
	}

	switch m.activationMode {
	case "auto":
		return m.activateAuto(ctx, deviceCode, compInfos)
	case "manual":
		return m.registerDevice(ctx, deviceCode, compInfos)
	case "both":
		if m.activationKey != "" {
			err := m.activateAuto(ctx, deviceCode, compInfos)
			if err == nil {
				return nil
			}
			slog.Warn("[activation] Auto-activation failed in both mode, falling back to manual", "error", err)
		}
		return m.registerDevice(ctx, deviceCode, compInfos)
	default:
		return fmt.Errorf("unknown activation mode: %s", m.activationMode)
	}
}

func (m *Manager) activateAuto(ctx context.Context, deviceCode string, components []map[string]string) error {
	if m.activationKey == "" {
		return fmt.Errorf("activation key not found in config")
	}

	slog.Info("[activation] Attempting auto-activation",
		"device_code", deviceCode,
		"platform_url", m.platformURL,
		"key_prefix", m.activationKey[:min(6, len(m.activationKey))]+"***",
	)

	body := map[string]interface{}{
		"activation_key": m.activationKey,
		"device_code":    deviceCode,
		"components":     components,
	}

	resp, err := m.postJSON(ctx, "/agent/v1/activate", body)
	if err != nil {
		return fmt.Errorf("activation request failed: %w", err)
	}

	if !resp.Activated {
		slog.Warn("[activation] Auto-activation rejected by platform", "error", resp.Error)
		return fmt.Errorf("activation rejected: %s", resp.Error)
	}

	m.status = &Status{
		Activated:      true,
		DeviceCode:     deviceCode,
		PackageID:      resp.PackageID,
		TenantID:       resp.TenantID,
		ActivationMode: "auto",
	}
	m.saveStatus()
	slog.Info("[activation] Auto-activated successfully", "device_code", deviceCode)
	return nil
}

func (m *Manager) registerDevice(ctx context.Context, deviceCode string, components []map[string]string) error {
	body := map[string]interface{}{
		"components": components,
	}

	_, err := m.postJSON(ctx, "/agent/v1/register-device", body)
	if err != nil {
		slog.Warn("[activation] Device registration failed (will retry)", "error", err)
	}

	m.status = &Status{
		Activated:      false,
		DeviceCode:     deviceCode,
		ActivationMode: "manual",
	}
	m.saveStatus()
	slog.Info("[activation] Device registered, waiting for admin binding", "device_code", deviceCode)
	return nil
}

// CheckStatus polls the server to see if the device has been activated (manual mode)
func (m *Manager) CheckStatus(ctx context.Context) error {
	if m.status == nil || m.status.DeviceCode == "" {
		return nil
	}

	url := fmt.Sprintf("%s/agent/v1/activation-status/%s", m.platformURL, m.status.DeviceCode)
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var result struct {
		Data struct {
			Activated      bool   `json:"activated"`
			PackageID      string `json:"package_id"`
			TenantID       string `json:"tenant_id"`
			ActivationMode string `json:"activation_mode"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}

	if result.Data.Activated && !m.status.Activated {
		m.status.Activated = true
		m.status.PackageID = result.Data.PackageID
		m.status.TenantID = result.Data.TenantID
		m.saveStatus()
		slog.Info("[activation] Device has been activated by admin", "device_code", m.status.DeviceCode)
	}

	return nil
}

// SendHeartbeat sends an activation heartbeat with current hardware components
func (m *Manager) SendHeartbeat(ctx context.Context) error {
	if m.status == nil || !m.status.Activated {
		return nil
	}

	components := hwinfo.CollectComponents()
	compInfos := make([]map[string]string, 0, len(components))
	for _, c := range components {
		compInfos = append(compInfos, map[string]string{"type": c.Type, "hash": c.Hash})
	}

	body := map[string]interface{}{
		"device_code": m.status.DeviceCode,
		"components":  compInfos,
	}

	resp, err := m.postJSON(ctx, "/agent/v1/activation-heartbeat", body)
	if err != nil {
		return err
	}
	if resp.Error == "revoked" {
		slog.Warn("[activation] Activation has been revoked by server")
		m.status.Activated = false
		m.saveStatus()
	}
	return nil
}

type activateResponse struct {
	Activated bool   `json:"activated"`
	PackageID string `json:"package_id"`
	TenantID  string `json:"tenant_id"`
	Error     string `json:"error"`
	Status    string `json:"status"`
}

func (m *Manager) postJSON(ctx context.Context, path string, body interface{}) (*activateResponse, error) {
	data, _ := json.Marshal(body)
	url := m.platformURL + path
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var wrapper struct {
		Data  activateResponse `json:"data"`
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&wrapper); err != nil {
		return nil, fmt.Errorf("failed to decode response (HTTP %d): %w", resp.StatusCode, err)
	}

	if resp.StatusCode >= 400 || wrapper.Error != nil {
		errMsg := fmt.Sprintf("HTTP %d", resp.StatusCode)
		if wrapper.Error != nil {
			errMsg = fmt.Sprintf("%s [%s]", wrapper.Error.Message, wrapper.Error.Code)
		}
		return &activateResponse{Error: errMsg}, nil
	}

	return &wrapper.Data, nil
}

func (m *Manager) loadStatus() {
	path := filepath.Join(m.configDir, "activation.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var s Status
	if json.Unmarshal(data, &s) == nil {
		m.status = &s
	}
}

func (m *Manager) saveStatus() {
	path := filepath.Join(m.configDir, "activation.json")
	_ = os.MkdirAll(m.configDir, 0755)
	data, _ := json.MarshalIndent(m.status, "", "  ")
	_ = os.WriteFile(path, data, 0600)
}

func generateDeviceCode(hwHash string) string {
	raw := sha256.Sum256([]byte(hwHash))
	encoded := base32Encode(raw[:10])
	if len(encoded) < 12 {
		encoded += "XXXXXXXXXXXX"
	}
	return fmt.Sprintf("ENX-%s-%s-%s", encoded[0:4], encoded[4:8], encoded[8:12])
}

func base32Encode(data []byte) string {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567"
	result := make([]byte, 0, len(data)*8/5+1)
	buffer := 0
	bitsLeft := 0
	for _, b := range data {
		buffer = (buffer << 8) | int(b)
		bitsLeft += 8
		for bitsLeft >= 5 {
			bitsLeft -= 5
			result = append(result, alphabet[(buffer>>bitsLeft)&0x1F])
		}
	}
	if bitsLeft > 0 {
		result = append(result, alphabet[(buffer<<(5-bitsLeft))&0x1F])
	}
	return string(result)
}
