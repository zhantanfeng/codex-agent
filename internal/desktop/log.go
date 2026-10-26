//go:build windows || darwin

package desktop

import (
	"os"
	"strings"
	"sync"
)

// Bound both the renderer history and on-disk logs. Logs stay on this PC.
type logBuffer struct {
	mu    sync.Mutex
	path  string
	file  *os.File
	size  int64
	lines []string
}

func newLogBuffer(path string) (*logBuffer, error) {
	b := &logBuffer{path: path, lines: []string{}}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	b.file = f
	if info, err := f.Stat(); err == nil {
		b.size = info.Size()
	}
	return b, nil
}

func (b *logBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.size+int64(len(p)) > 1024*1024 {
		_ = b.file.Close()
		_ = os.Remove(b.path + ".1")
		_ = os.Rename(b.path, b.path+".1")
		f, err := os.OpenFile(b.path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
		if err != nil {
			return 0, err
		}
		b.file, b.size = f, 0
	}
	n, err := b.file.Write(p)
	b.size += int64(n)
	for _, line := range strings.Split(strings.TrimSpace(string(p)), "\n") {
		if len(line) > 4000 {
			line = line[:4000] + "…"
		}
		b.lines = append(b.lines, line)
	}
	if len(b.lines) > 200 {
		b.lines = append([]string{}, b.lines[len(b.lines)-200:]...)
	}
	return n, err
}
func (b *logBuffer) snapshot() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string{}, b.lines...)
}
func (b *logBuffer) close() { b.mu.Lock(); defer b.mu.Unlock(); _ = b.file.Close() }
