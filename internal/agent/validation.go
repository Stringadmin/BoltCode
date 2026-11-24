package agent

import (
	"context"
	"os"
	"strings"
)

const checkLogLimit = 64 << 10

// RunValidationCommand uses the same project shell and scrubbed environment
// as the agent. The user explicitly configures and starts this command.
func RunValidationCommand(ctx context.Context, dir, command, apiKey string) (string, error) {
	cmd := shellCommand(ctx, command, dir, scrubEnv(os.Environ(), apiKey), true)
	log := &checkLog{}
	cmd.Stdout, cmd.Stderr = log, log
	err := cmd.Run()
	text := strings.ToValidUTF8(string(log.tail), "�")
	if log.truncated {
		text = "… 日志已截断，显示最后 64 KB\n" + text
	}
	return strings.TrimSpace(text), err
}

type checkLog struct {
	tail      []byte
	truncated bool
}

func (l *checkLog) Write(data []byte) (int, error) {
	size := len(data)
	if size >= checkLogLimit {
		l.truncated = l.truncated || size > checkLogLimit || len(l.tail) > 0
		l.tail = append(l.tail[:0], data[size-checkLogLimit:]...)
		return size, nil
	}
	if discard := len(l.tail) + size - checkLogLimit; discard > 0 {
		copy(l.tail, l.tail[discard:])
		l.tail = l.tail[:len(l.tail)-discard]
		l.truncated = true
	}
	l.tail = append(l.tail, data...)
	return size, nil
}
