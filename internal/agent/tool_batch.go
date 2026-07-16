package agent

import (
	"sync"
	"time"
)

type toolExecution struct {
	call    ToolCall
	started Event
	result  ToolResult
	elapsed time.Duration
}

// parallelReadTool is intentionally narrower than the complete read-only mode
// allowlist. These tools only read the local filesystem and do not touch the
// Agent's cancellation slot, git subprocesses, network clients, or mutable
// session state, so they are safe to execute concurrently.
func parallelReadTool(name string) bool {
	switch name {
	case "read_file", "search_code", "list_files", "glob":
		return true
	default:
		return false
	}
}

func canParallelizeToolCalls(calls []ToolCall) bool {
	if len(calls) < 2 {
		return false
	}
	for _, call := range calls {
		if !parallelReadTool(call.Name) {
			return false
		}
	}
	return true
}

func (a *Agent) executeReadToolsParallel(calls []ToolCall) []toolExecution {
	executions := make([]toolExecution, len(calls))
	for index, call := range calls {
		a.Stats.AddToolCall()
		started := a.toolEvent(call)
		a.emit(started)
		a.printToolUse(call)
		executions[index] = toolExecution{call: call, started: started}
	}

	var wait sync.WaitGroup
	wait.Add(len(executions))
	for index := range executions {
		go func(index int) {
			defer wait.Done()
			start := time.Now()
			executions[index].result = a.ExecTool(executions[index].call)
			executions[index].elapsed = time.Since(start)
		}(index)
	}
	wait.Wait()
	return executions
}

func (a *Agent) finishToolExecution(execution toolExecution) (map[string]interface{}, bool) {
	a.printToolResult(execution.call, execution.result, execution.elapsed)
	finishedEvent := Event{
		Kind:     EventToolFinished,
		Activity: "Reviewing tool result",
		ToolID:   execution.call.ID,
		ToolName: execution.call.Name,
		Detail:   execution.started.Detail,
		Result:   execution.result.Content,
		IsError:  execution.result.IsError,
		Duration: execution.elapsed,
	}
	a.addPlanState(&finishedEvent)
	a.emit(finishedEvent)
	return map[string]interface{}{
		"type":        "tool_result",
		"tool_use_id": execution.result.ToolUseID,
		"content":     execution.result.Content,
		"is_error":    execution.result.IsError,
	}, execution.result.IsError
}
