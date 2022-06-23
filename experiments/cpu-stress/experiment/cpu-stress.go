// Copyright 2022 LitmusChaos Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// See the License for the specific language governing permissions and
// limitations under the License.

package experiment

import (
	"bytes"
	"fmt"
	"net/http"
	"os/exec"

	"github.com/litmuschaos/m-agent/internal/m-agent/errorcodes"
	logger "github.com/litmuschaos/m-agent/internal/m-agent/log"
	"github.com/litmuschaos/m-agent/internal/m-agent/messages"
	"github.com/litmuschaos/m-agent/internal/m-agent/upgrader"
	"github.com/litmuschaos/m-agent/pkg/cpu"
	"github.com/litmuschaos/m-agent/pkg/probes"
	stressng "github.com/litmuschaos/m-agent/pkg/stress-ng"
	"github.com/pkg/errors"
)

var (
	stdout bytes.Buffer
	stderr bytes.Buffer
	cmd    *exec.Cmd
)

// CPUStress listens for the client actions and executes them as appropriate
func CPUStress(w http.ResponseWriter, r *http.Request) {

	// upgrade the connection to a websocket connection
	upgrader := upgrader.GetConnectionUpgrader()

	clientMessageReadLogger := logger.GetClientMessageReadErrorLogger()
	steadyStateCheckErrorLogger := logger.GetSteadyStateCheckErrorLogger()
	executeExperimentErrorLogger := logger.GetExecuteExperimentErrorLogger()
	commandProbeExecutionErrorLogger := logger.GetCommandProbeExecutionErrorLogger()
	invalidActionErrorLogger := logger.GetCommandProbeExecutionErrorLogger()
	chaosAbortErrorLogger := logger.GetChaosAbortErrorLogger()
	livenessCheckErrorLogger := logger.GetLivenessCheckErrorLogger()
	closeConnectionErrorLogger := logger.GetCloseConnectionErrorLogger()
	chaosRevertErrorLogger := logger.GetChaosRevertErrorLogger()

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		fmt.Fprintf(w, "Failed to establish connection with client, err: %v", err)
		return
	}

	for {

		action, reqID, payload, err := messages.ListenForClientMessage(conn)
		if err != nil {
			messages.HandleActionExecutionError(conn, reqID, errorcodes.GetClientMessageReadErrorPrefix(), err, clientMessageReadLogger)
			return
		}

		switch action {

		case "CHECK_STEADY_STATE":
			if err := stressng.CheckStressNG(); err != nil {
				messages.HandleActionExecutionError(conn, reqID, errorcodes.GetSteadyStateCheckErrorPrefix(), err, steadyStateCheckErrorLogger)
				return
			}

			if err := messages.SendMessageToClient(conn, "ACTION_SUCCESSFUL", reqID, nil); err != nil {
				messages.HandleFeedbackTransmissionError(conn, err, steadyStateCheckErrorLogger)
				return
			}

		case "EXECUTE_EXPERIMENT":
			cmd, err = cpu.StressCPU(payload, &stdout, &stderr)

			if err != nil {
				messages.HandleActionExecutionError(conn, reqID, errorcodes.GetExecuteExperimentErrorPrefix(), err, executeExperimentErrorLogger)
				return
			}

			if err := messages.SendMessageToClient(conn, "ACTION_SUCCESSFUL", reqID, nil); err != nil {
				messages.HandleFeedbackTransmissionError(conn, err, executeExperimentErrorLogger)
				return
			}

		case "CHECK_LIVENESS":
			if err := stressng.CheckStressNGProcessLiveness(cmd.Process.Pid); err != nil {
				messages.HandleActionExecutionError(conn, reqID, errorcodes.GetLivenessCheckErrorPrefix(), err, livenessCheckErrorLogger)
				return
			}

			if err := messages.SendMessageToClient(conn, "ACTION_SUCCESSFUL", reqID, nil); err != nil {
				messages.HandleFeedbackTransmissionError(conn, err, livenessCheckErrorLogger)
				return
			}

		case "EXECUTE_COMMAND":
			cmdProbeStdout, err := probes.ExecuteCmdProbeCommand(payload)

			if err != nil {

				if err := messages.SendMessageToClient(conn, "PROBE_ERROR", reqID, errorcodes.GetCommandProbeExecutionErrorPrefix()+err.Error()); err != nil {
					messages.HandleFeedbackTransmissionError(conn, err, commandProbeExecutionErrorLogger)
					return
				}

			} else {

				if err := messages.SendMessageToClient(conn, "ACTION_SUCCESSFUL", reqID, cmdProbeStdout); err != nil {
					messages.HandleFeedbackTransmissionError(conn, err, commandProbeExecutionErrorLogger)
					return
				}
			}

		case "REVERT_CHAOS":
			if err := stressng.RevertStressNGProcess(cmd, &stderr); err != nil {
				messages.HandleActionExecutionError(conn, reqID, errorcodes.GetChaosRevertErrorPrefix(), err, chaosRevertErrorLogger)
				return
			}

			if err := messages.SendMessageToClient(conn, "ACTION_SUCCESSFUL", reqID, stdout.String()); err != nil {
				messages.HandleFeedbackTransmissionError(conn, err, chaosRevertErrorLogger)
				return
			}

		case "ABORT_EXPERIMENT":
			if err := stressng.AbortStressNGProcess(cmd); err != nil {
				messages.HandleActionExecutionError(conn, reqID, errorcodes.GetChaosAbortErrorPrefix(), err, chaosAbortErrorLogger)
				return
			}

			if err := messages.SendMessageToClient(conn, "ACTION_SUCCESSFUL", reqID, nil); err != nil {
				messages.HandleFeedbackTransmissionError(conn, err, chaosAbortErrorLogger)
				return
			}

			if err := messages.SendMessageToClient(conn, "CLOSE_CONNECTION", reqID, nil); err != nil {
				messages.HandleFeedbackTransmissionError(conn, err, chaosAbortErrorLogger)
				return
			}

			if err := conn.Close(); err != nil {
				chaosAbortErrorLogger.Printf("Error occured while closing the connection, err: %v", err)
			}

			return

		case "CLOSE_CONNECTION":
			if err := messages.SendMessageToClient(conn, "CLOSE_CONNECTION", reqID, nil); err != nil {
				closeConnectionErrorLogger.Printf("Error occured while sending feedback message to client, err: %v", err)
			}

			if err := conn.Close(); err != nil {
				closeConnectionErrorLogger.Printf("Error occured while closing the connection, err: %v", err)
			}

			return

		default:
			messages.HandleActionExecutionError(conn, reqID, errorcodes.GetInvalidActionErrorPrefix(), errors.Errorf("Invalid action: "+action), invalidActionErrorLogger)
			return
		}
	}
}
