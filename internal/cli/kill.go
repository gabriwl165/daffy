package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/gabriwl165/daffy/internal/distribution"
)

type killSelector string

const (
	killPID  killSelector = "pid"
	killPort killSelector = "port"
)

func runKill(ctx context.Context, args []string, out, errOut io.Writer, controller distribution.ProcessController) int {
	if isHelp(args) || (len(args) == 1 && args[0] == "help") {
		_, err := fmt.Fprint(out, "Usage: daffy kill pid N | kill port N\nSends SIGTERM to one process; port selects TCP listeners only.\n")
		if err != nil {
			return failed(errOut, err)
		}
		return int(exitSuccess)
	}
	if len(args) != 2 {
		fmt.Fprintln(errOut, "usage: daffy kill pid N | kill port N")
		return int(exitUsage)
	}
	selector := killSelector(args[0])
	if selector != killPID && selector != killPort {
		fmt.Fprintf(errOut, "unknown kill selector %q\n", args[0])
		return int(exitUsage)
	}
	n := args[1]
	for _, r := range n {
		if r < '0' || r > '9' {
			fmt.Fprintln(errOut, "target must be a decimal number")
			return int(exitUsage)
		}
	}
	if n == "" {
		fmt.Fprintln(errOut, "target must be a decimal number")
		return int(exitUsage)
	}
	v, err := strconv.ParseInt(n, 10, strconv.IntSize)
	if err != nil {
		fmt.Fprintln(errOut, "target is outside the supported range")
		return int(exitUsage)
	}
	target := distribution.ProcessTarget{}
	if selector == killPID {
		if v <= 1 {
			fmt.Fprintln(errOut, "PID must be greater than 1")
			return int(exitUsage)
		}
		target = distribution.ProcessTarget{Kind: distribution.TargetPID, PID: int(v)}
	} else {
		if v < 1 || v > 65535 {
			fmt.Fprintln(errOut, "port must be between 1 and 65535")
			return int(exitUsage)
		}
		target = distribution.ProcessTarget{Kind: distribution.TargetTCPPort, Port: int(v)}
	}
	if err := ctx.Err(); err != nil {
		return interrupted(errOut, err)
	}
	if controller == nil {
		return failed(errOut, errors.New("process controller unavailable"))
	}
	result, err := controller.Terminate(ctx, target)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return interrupted(errOut, err)
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return failed(errOut, errors.New("process operation timed out"))
		}
		var pe *distribution.ProcessError
		if errors.As(err, &pe) {
			switch pe.Kind {
			case distribution.ProcessAmbiguous:
				fmt.Fprintf(errOut, "multiple processes own target (PIDs: %s); refusing to signal\n", joinPIDs(pe.PIDs))
			case distribution.ProcessDiscoveryFailed:
				fmt.Fprintf(errOut, "process discovery incomplete; refusing to signal: %s\n", boundedError(pe.Err))
			case distribution.ProcessNotFound:
				fmt.Fprintln(errOut, "no matching process found")
			case distribution.ProcessIdentityChanged:
				fmt.Fprintf(errOut, "process identity changed (PIDs: %s); refusing to signal\n", joinPIDs(pe.PIDs))
			case distribution.ProcessInvalidTarget:
				fmt.Fprintln(errOut, "invalid process target")
			case distribution.ProcessSignalFailed:
				if errors.Is(pe.Err, os.ErrPermission) {
					fmt.Fprintln(errOut, "permission denied sending SIGTERM; refusing to retry with elevated privileges")
				} else {
					fmt.Fprintf(errOut, "could not send SIGTERM: %s\n", boundedError(pe.Err))
				}
			default:
				fmt.Fprintln(errOut, "process operation failed")
			}
			return int(exitFailure)
		}
		return failed(errOut, errors.New("process operation failed"))
	}
	if result.PID <= 1 {
		return failed(errOut, errors.New("process controller returned an invalid PID"))
	}
	if result.Signal != distribution.SignalTerminate {
		return failed(errOut, errors.New("unexpected process signal result"))
	}
	if _, err := fmt.Fprintf(out, "Sent SIGTERM to PID %d\n", result.PID); err != nil {
		return failed(errOut, err)
	}
	return int(exitSuccess)
}

func boundedError(err error) string {
	if err == nil {
		return "additional process information unavailable (check lsof availability and permissions)"
	}
	s := safe(err.Error())
	if len(s) > 240 {
		s = s[:240]
	}
	if s == "" {
		return "additional process information unavailable"
	}
	return s
}

func joinPIDs(pids []int) string {
	parts := make([]string, len(pids))
	for i, pid := range pids {
		parts[i] = strconv.Itoa(pid)
	}
	return strings.Join(parts, ", ")
}
