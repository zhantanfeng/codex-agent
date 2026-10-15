package agent

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

type directoryEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type directoryPage struct {
	Path       string           `json:"path"`
	Parent     string           `json:"parent"`
	Entries    []directoryEntry `json:"entries"`
	NextOffset *int             `json:"nextOffset"`
}

func listDirectories(ctx context.Context, raw json.RawMessage) (*directoryPage, error) {
	var request struct {
		Path   string `json:"path"`
		Offset int    `json:"offset"`
		Limit  int    `json:"limit"`
	}
	if err := json.Unmarshal(raw, &request); err != nil {
		return nil, err
	}
	if request.Offset < 0 || request.Limit < 0 || request.Limit > 500 {
		return nil, errors.New("目录分页参数无效")
	}
	if request.Limit == 0 {
		request.Limit = 200
	}
	page := &directoryPage{Entries: []directoryEntry{}}
	var entries []directoryEntry
	if request.Path == "" {
		var err error
		entries, err = filesystemRoots()
		if err != nil {
			return nil, fmt.Errorf("无法获取电脑磁盘列表：%w", err)
		}
	} else {
		path, err := readableDirectory(request.Path)
		if err != nil {
			return nil, err
		}
		page.Path = path
		if parent := filepath.Dir(path); parent != path {
			page.Parent = parent
		}
		children, err := os.ReadDir(path)
		if err != nil {
			return nil, directoryError(path, err)
		}
		for _, child := range children {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			isDir := child.IsDir()
			if child.Type()&os.ModeSymlink != 0 {
				info, err := os.Stat(filepath.Join(path, child.Name()))
				isDir = err == nil && info.IsDir()
			}
			if isDir {
				entries = append(entries, directoryEntry{Name: child.Name(), Path: filepath.Join(path, child.Name())})
			}
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		left, right := strings.ToLower(entries[i].Name), strings.ToLower(entries[j].Name)
		if left == right {
			return entries[i].Name < entries[j].Name
		}
		return left < right
	})
	start := min(request.Offset, len(entries))
	end := start + min(request.Limit, len(entries)-start)
	page.Entries = append(page.Entries, entries[start:end]...)
	if end < len(entries) {
		page.NextOffset = &end
	}
	return page, ctx.Err()
}

func readableDirectory(path string) (string, error) {
	if !filepath.IsAbs(path) || strings.ContainsRune(path, '\x00') || len(path) > 32767 {
		return "", errors.New("请输入完整的电脑目录路径，例如 D:\\projects；不支持相对路径")
	}
	if runtime.GOOS == "windows" && (strings.HasPrefix(path, `\\.\`) || strings.HasPrefix(path, `\\?\`)) {
		return "", errors.New("不支持 Windows 设备路径，请使用普通磁盘路径")
	}
	path = filepath.Clean(path)
	info, err := os.Stat(path)
	if err != nil {
		return "", directoryError(path, err)
	}
	if !info.IsDir() {
		return "", errors.New("所选路径不是文件夹，请选择一个目录")
	}
	folder, err := os.Open(path)
	if err != nil {
		return "", directoryError(path, err)
	}
	_ = folder.Close()
	return path, nil
}

func directoryError(path string, err error) error {
	switch {
	case errors.Is(err, os.ErrNotExist):
		return fmt.Errorf("目录不存在或磁盘未连接：%s", path)
	case errors.Is(err, os.ErrPermission):
		return fmt.Errorf("电脑当前账户没有权限访问该目录：%s", path)
	default:
		return fmt.Errorf("无法读取目录 %s：%w", path, err)
	}
}

// Only the explicitly selected folder is added, not its parent or entire drive.
// Keeping it in Projects preserves access to this session after an Agent restart.
func (r *Runtime) rememberDirectory(path string) error {
	config := r.currentConfig()
	if r.store != nil && r.store.ConfigPath != "" {
		latest, err := r.store.LoadConfig()
		if err != nil {
			return err
		}
		config = latest
	}
	for _, known := range config.Projects {
		if sameDirectory(path, known) {
			r.setConfig(config)
			return nil
		}
	}
	copyConfig := *config
	copyConfig.Projects = make(map[string]string, len(config.Projects)+1)
	for id, known := range config.Projects {
		copyConfig.Projects[id] = known
	}
	key := path
	if runtime.GOOS == "windows" {
		key = strings.ToLower(key)
	}
	digest := sha256.Sum256([]byte(key))
	id := fmt.Sprintf("phone-%x", digest[:8])
	for suffix := 1; copyConfig.Projects[id] != ""; suffix++ {
		id = fmt.Sprintf("phone-%x-%d", digest[:8], suffix)
	}
	copyConfig.Projects[id] = path
	if r.store != nil && r.store.ConfigPath != "" {
		if err := r.store.Save(&copyConfig); err != nil {
			return fmt.Errorf("无法保存所选目录，请重试：%w", err)
		}
	}
	r.setConfig(&copyConfig)
	return nil
}

func sameDirectory(left, right string) bool {
	left, right = filepath.Clean(left), filepath.Clean(right)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}
