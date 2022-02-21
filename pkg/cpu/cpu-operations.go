package cpu

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"

	"github.com/gorilla/websocket"
	"github.com/litmuschaos/m-agent/internal/m-agent/messages"

	"github.com/pkg/errors"
)

// StressCPU starts a stress-ng process in background and returns the exec cmd for it
func StressCPU(payload []byte, reqID string, conn *websocket.Conn) (*exec.Cmd, error) {

	var (
		stdout bytes.Buffer
		stderr bytes.Buffer
	)

	type CPUStressParams struct {
		Workers string
		Load    string
		Timeout string
	}

	var cpuStressParams CPUStressParams

	if err := json.Unmarshal(payload, &cpuStressParams); err != nil {
		return nil, err
	}

	stressCommand := fmt.Sprintf("stress-ng --cpu %s --cpu-load %s --timeout %s", cpuStressParams.Workers, cpuStressParams.Load, cpuStressParams.Timeout)

	cmd := exec.Command("bash", "-c", stressCommand)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// if err := cmd.Start(); err != nil {
	// 	return nil, errors.Errorf("%s, stderr: %s", err, stderr.String())
	// }

	// return cmd, nil

	go func(cmd *exec.Cmd, stderr, stdout *bytes.Buffer) {

		if err := cmd.Run(); err != nil {

			messages.SendMessageToClient(conn, "ERROR", reqID, errors.Errorf("stress-ng process failed during execution, err: %s; stderr: %s", err.Error(), stderr.String()))

			conn.Close()
		}

		messages.SendMessageToClient(conn, "ACTION_SUCCESSFUL", reqID, stdout.String())
	}(cmd, &stderr, &stdout)

	return cmd, nil
}
