package agent

import (
	"context"
	"errors"
	"strings"
)

type codexCall func(context.Context, string, any, any) error

func resumeOrReadThread(ctx context.Context, threadID string, call codexCall) (map[string]any, error) {
	var result map[string]any
	err := call(ctx, "thread/resume", map[string]any{"threadId": threadID, "excludeTurns": false}, &result)
	if err == nil {
		result["readOnly"] = false
		return result, nil
	}
	if !strings.Contains(strings.ToLower(err.Error()), "already has an active writer") {
		return nil, err
	}
	// Reading persisted history does not acquire or replace the other client's writer.
	result = nil
	if err := call(ctx, "thread/read", map[string]any{"threadId": threadID, "includeTurns": true}, &result); err != nil {
		return nil, err
	}
	result["readOnly"] = true
	return result, nil
}

func (r *Runtime) setThreadReadOnly(threadID string, readOnly bool) {
	r.threadMu.Lock()
	defer r.threadMu.Unlock()
	if r.readOnlyThreads == nil {
		r.readOnlyThreads = map[string]bool{}
	}
	if readOnly {
		r.readOnlyThreads[threadID] = true
	} else {
		delete(r.readOnlyThreads, threadID)
	}
}

func (r *Runtime) requireWritableThread(threadID string) error {
	r.threadMu.Lock()
	defer r.threadMu.Unlock()
	if !r.threads[threadID] {
		return errors.New("thread is not in an allowed project")
	}
	if r.readOnlyThreads[threadID] {
		return errors.New("此会话正在其他客户端中使用，当前仅可查看。请在另一端退出后重试。")
	}
	return nil
}
