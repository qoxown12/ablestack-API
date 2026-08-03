package glueservice

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const defaultCommandTimeout = 60 * time.Second

// CommandError는 handler가 실패한 명령 정보를 확인할 수 있게 하되,
// exec 내부 세부 정보를 응답 필드로 과하게 노출하지 않도록 묶어둔다.
type CommandError struct {
	Command  string
	Args     []string
	Output   string
	TimedOut bool
	Err      error
}

func (e CommandError) Error() string {
	output := strings.TrimSpace(e.Output)
	if output == "" {
		output = strings.TrimSpace(e.Err.Error())
	}
	if e.TimedOut {
		return fmt.Sprintf("%s timed out: %s", commandLine(e.Command, e.Args), output)
	}
	return fmt.Sprintf("%s failed: %s", commandLine(e.Command, e.Args), output)
}

func (e CommandError) Unwrap() error {
	return e.Err
}

type commandRunner func(ctx context.Context, command string, args ...string) ([]byte, bool, error)

// runCommand는 테스트에서 교체할 수 있는 실행 지점이다. 운영 코드는 runLocalCommand를 사용한다.
var runCommand commandRunner = runLocalCommand

// runLocalCommand는 Glue API가 OS 명령을 실행하는 유일한 지점이다.
// shell을 거치지 않아 legacy grep/cut 방식의 command injection 표면을 없앤다.
func runLocalCommand(ctx context.Context, command string, args ...string) ([]byte, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, defaultCommandTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, command, args...)
	output, err := cmd.CombinedOutput()
	return output, errors.Is(ctx.Err(), context.DeadlineExceeded), err
}

// run은 성공한 출력의 공백을 정리하고, 실패 시 명령 메타데이터를 함께 감싼다.
func run(ctx context.Context, command string, args ...string) ([]byte, error) {
	output, timedOut, err := runCommand(ctx, command, args...)
	if err != nil {
		return nil, CommandError{
			Command:  command,
			Args:     args,
			Output:   string(output),
			TimedOut: timedOut,
			Err:      err,
		}
	}
	return bytes.TrimSpace(output), nil
}

// runJSON은 안정적인 JSON 출력을 제공하는 Ceph 명령에 사용한다.
func runJSON(ctx context.Context, command string, args ...string) (any, error) {
	output, err := run(ctx, command, args...)
	if err != nil {
		return nil, err
	}
	return decodeJSON(output)
}

func decodeJSON(output []byte) (any, error) {
	if len(output) == 0 {
		return nil, nil
	}
	var value any
	if err := json.Unmarshal(output, &value); err != nil {
		return nil, fmt.Errorf("decode command json output: %w", err)
	}
	return value, nil
}

func decodeJSONStringSlice(output []byte) ([]string, error) {
	if len(output) == 0 {
		return []string{}, nil
	}
	values := []string{}
	if err := json.Unmarshal(output, &values); err != nil {
		return nil, fmt.Errorf("decode command json output: %w", err)
	}
	return values, nil
}

func commandLine(command string, args []string) string {
	parts := append([]string{command}, args...)
	return strings.Join(parts, " ")
}
