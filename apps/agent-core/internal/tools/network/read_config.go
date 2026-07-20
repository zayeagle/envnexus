package network

import (
	"context"
	"net"
	"time"

	"github.com/zayeagle/envnexus/apps/agent-core/internal/tools"
)

type ReadNetworkConfigTool struct{}

func NewReadNetworkConfigTool() *ReadNetworkConfigTool {
	return &ReadNetworkConfigTool{}
}

func (t *ReadNetworkConfigTool) Name() string {
	return "read_network_config"
}

func (t *ReadNetworkConfigTool) Description() string {
	return "Reads the local network interfaces and IP addresses."
}

func (t *ReadNetworkConfigTool) IsReadOnly() bool {
	return true
}

func (t *ReadNetworkConfigTool) RiskLevel() string {
	return "L0"
}

func (t *ReadNetworkConfigTool) Parameters() *tools.ParamSchema {
	return tools.NoParams()
}

func (t *ReadNetworkConfigTool) Execute(ctx context.Context, params map[string]interface{}) (*tools.ToolResult, error) {
	start := time.Now()

	interfaces, err := net.Interfaces()
	if err != nil {
		return &tools.ToolResult{
			ToolName:   t.Name(),
			Status:     "failed",
			Error:      err.Error(),
			DurationMs: time.Since(start).Milliseconds(),
		}, nil
	}

	var result []map[string]interface{}
	for _, iface := range interfaces {
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		
		var ipList []string
		for _, addr := range addrs {
			ipList = append(ipList, addr.String())
		}

		result = append(result, map[string]interface{}{
			"name":          iface.Name,
			"mac_address":   iface.HardwareAddr.String(),
			"ip_addresses":  ipList,
			"flags":         iface.Flags.String(),
		})
	}

	return &tools.ToolResult{
		ToolName:   t.Name(),
		Status:     "succeeded",
		Summary:    "Successfully read network interfaces",
		Output:     result,
		DurationMs: time.Since(start).Milliseconds(),
	}, nil
}
