package database

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"time"

	"github.com/zayeagle/envnexus/apps/agent-core/internal/tools"
)

type MongoCheckTool struct{}

func NewMongoCheckTool() *MongoCheckTool { return &MongoCheckTool{} }

func (t *MongoCheckTool) Name() string { return "mongo_check" }
func (t *MongoCheckTool) Description() string {
	return "Checks MongoDB connectivity. Params: host (default 127.0.0.1), port (default 27017). Tests TCP connectivity and sends a basic wire protocol probe — no credentials required."
}
func (t *MongoCheckTool) IsReadOnly() bool  { return true }
func (t *MongoCheckTool) RiskLevel() string { return "L0" }

func (t *MongoCheckTool) Parameters() *tools.ParamSchema {
	return &tools.ParamSchema{
		Type: "object",
		Properties: map[string]tools.ParamProperty{
			"host": {
				Type:        "string",
				Description: "MongoDB host, default 127.0.0.1",
			},
			"port": {
				Type:        "string",
				Description: "MongoDB port, default 27017",
			},
		},
	}
}

func (t *MongoCheckTool) Execute(ctx context.Context, params map[string]interface{}) (*tools.ToolResult, error) {
	host, _ := params["host"].(string)
	if host == "" {
		host = "127.0.0.1"
	}
	port := "27017"
	if p, ok := params["port"].(string); ok && p != "" {
		port = p
	} else if pf, ok := params["port"].(float64); ok {
		port = fmt.Sprintf("%d", int(pf))
	}

	start := time.Now()
	addr := net.JoinHostPort(host, port)
	result := map[string]interface{}{
		"host": host,
		"port": port,
	}

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	elapsed := time.Since(start)

	if err != nil {
		result["reachable"] = false
		result["error"] = err.Error()
		return &tools.ToolResult{
			ToolName:   t.Name(),
			Status:     "succeeded",
			Summary:    fmt.Sprintf("MongoDB at %s is unreachable: %v", addr, err),
			Output:     result,
			DurationMs: elapsed.Milliseconds(),
		}, nil
	}
	defer conn.Close()

	result["reachable"] = true
	result["connect_ms"] = elapsed.Milliseconds()

	// Send a minimal OP_MSG with {"isMaster": 1} to probe the MongoDB server.
	// This is a simplified probe — we only check if the server responds.
	isMasterCmd := buildIsMasterMsg()
	conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
	_, err = conn.Write(isMasterCmd)
	if err != nil {
		result["protocol_check"] = "write_failed"
		result["protocol_error"] = err.Error()
	} else {
		conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		header := make([]byte, 4)
		_, err = conn.Read(header)
		if err != nil {
			result["protocol_check"] = "read_failed"
			result["protocol_error"] = err.Error()
		} else {
			respLen := binary.LittleEndian.Uint32(header)
			if respLen > 0 && respLen < 65536 {
				result["protocol_check"] = "ok"
				result["response_size"] = respLen
			} else {
				result["protocol_check"] = "unexpected_response"
			}
		}
	}

	summary := fmt.Sprintf("MongoDB at %s reachable (connect: %dms)", addr, elapsed.Milliseconds())
	if result["protocol_check"] == "ok" {
		summary += ", protocol handshake ok"
	}

	return &tools.ToolResult{
		ToolName:   t.Name(),
		Status:     "succeeded",
		Summary:    summary,
		Output:     result,
		DurationMs: time.Since(start).Milliseconds(),
	}, nil
}

// buildIsMasterMsg builds a minimal MongoDB OP_MSG for {isMaster: 1, $db: "admin"}.
func buildIsMasterMsg() []byte {
	// BSON document: {isMaster: 1, $db: "admin"}
	bson := []byte{
		// isMaster: 1 (int32)
		0x10, // type: int32
	}
	bson = append(bson, []byte("isMaster")...)
	bson = append(bson, 0x00)                         // null terminator
	bson = append(bson, 0x01, 0x00, 0x00, 0x00)       // value: 1

	// $db: "admin"
	bson = append(bson, 0x02) // type: string
	bson = append(bson, []byte("$db")...)
	bson = append(bson, 0x00)                         // null terminator
	bson = append(bson, 0x06, 0x00, 0x00, 0x00)       // string length (5 + 1 null)
	bson = append(bson, []byte("admin")...)
	bson = append(bson, 0x00) // null terminator

	bson = append(bson, 0x00) // document terminator

	// Prepend BSON document length
	docLen := make([]byte, 4)
	binary.LittleEndian.PutUint32(docLen, uint32(4+len(bson)))
	bsonDoc := append(docLen, bson...)

	// OP_MSG section: kind=0 (body) + BSON document
	section := append([]byte{0x00}, bsonDoc...)

	// OP_MSG: flagBits (4 bytes, all zero) + section
	msgBody := append([]byte{0x00, 0x00, 0x00, 0x00}, section...)

	// MsgHeader: messageLength (4) + requestID (4) + responseTo (4) + opCode (4=OP_MSG=2013)
	headerLen := 16
	totalLen := headerLen + len(msgBody)

	msg := make([]byte, 4)
	binary.LittleEndian.PutUint32(msg, uint32(totalLen))
	msg = append(msg, 0x01, 0x00, 0x00, 0x00) // requestID = 1
	msg = append(msg, 0x00, 0x00, 0x00, 0x00) // responseTo = 0
	msg = append(msg, 0xDD, 0x07, 0x00, 0x00) // opCode = 2013 (OP_MSG)
	msg = append(msg, msgBody...)

	return msg
}
