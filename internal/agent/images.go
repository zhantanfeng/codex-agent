package agent

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const maxImageBytes = 8 << 20
const imageChunkBytes = 192 << 10
const maxImagesPerMessage = 4

var imageIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,80}$`)

type imageRecord struct {
	ID       string `json:"id"`
	ThreadID string `json:"threadId"`
	ClientID string `json:"clientId"`
	Name     string `json:"name"`
	MimeType string `json:"mimeType"`
	Size     int64  `json:"size"`
	SHA256   string `json:"sha256"`
	Complete bool   `json:"complete"`
}

func (r *Runtime) imageBase(threadID, id string) (string, error) {
	if r.store == nil || !r.threadAllowed(threadID) || !imageIDPattern.MatchString(id) {
		return "", errors.New("图片不属于可访问的会话")
	}
	threadHash := sha256.Sum256([]byte(threadID))
	return filepath.Join(r.store.Dir, "images", hex.EncodeToString(threadHash[:]), id), nil
}

func imageExtension(mime string) string {
	if mime == "image/png" {
		return ".png"
	}
	return ".jpg"
}

func loadImageRecord(base string) (imageRecord, error) {
	var record imageRecord
	raw, err := os.ReadFile(base + ".json")
	if err != nil {
		return record, err
	}
	err = json.Unmarshal(raw, &record)
	return record, err
}

func saveImageRecord(base string, record imageRecord) error {
	raw, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if err := os.WriteFile(base+".json.tmp", raw, 0o600); err != nil {
		return err
	}
	return os.Rename(base+".json.tmp", base+".json")
}

func (r *Runtime) imageCommand(command commandPayload) (any, error) {
	var data struct {
		ID       string `json:"id"`
		ThreadID string `json:"threadId"`
		Name     string `json:"name"`
		MimeType string `json:"mimeType"`
		Size     int64  `json:"size"`
		SHA256   string `json:"sha256"`
		Offset   int64  `json:"offset"`
		Bytes    string `json:"bytes"`
	}
	if err := json.Unmarshal(command.Data, &data); err != nil {
		return nil, err
	}
	base, err := r.imageBase(data.ThreadID, data.ID)
	if err != nil {
		return nil, err
	}
	record, loadErr := loadImageRecord(base)
	if command.Action == "image.begin" {
		digest, digestErr := hex.DecodeString(data.SHA256)
		if data.Size <= 0 || data.Size > maxImageBytes || (data.MimeType != "image/jpeg" && data.MimeType != "image/png") || digestErr != nil || len(digest) != sha256.Size {
			return nil, errors.New("请选择不超过 8 MB 的 JPEG 或 PNG 图片")
		}
		if loadErr == nil {
			if record.ThreadID != data.ThreadID || record.ClientID != command.clientID || record.Size != data.Size || record.SHA256 != data.SHA256 || record.MimeType != data.MimeType {
				return nil, errors.New("图片上传信息不匹配，请重新选择图片")
			}
		} else {
			if !errors.Is(loadErr, os.ErrNotExist) {
				return nil, loadErr
			}
			if err := os.MkdirAll(filepath.Dir(base), 0o700); err != nil {
				return nil, err
			}
			record = imageRecord{ID: data.ID, ThreadID: data.ThreadID, ClientID: command.clientID, Name: data.Name, MimeType: data.MimeType, Size: data.Size, SHA256: data.SHA256}
			file, err := os.OpenFile(base+".part", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
			if err != nil {
				return nil, err
			}
			_ = file.Close()
			if err := saveImageRecord(base, record); err != nil {
				return nil, err
			}
		}
		path := base + ".part"
		if record.Complete {
			path = base + imageExtension(record.MimeType)
		}
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		return map[string]any{"offset": info.Size(), "complete": record.Complete}, nil
	}
	if loadErr != nil || record.ThreadID != data.ThreadID {
		return nil, errors.New("图片上传已失效，请重新上传")
	}
	if command.Action == "image.thumbnail" {
		if !record.Complete {
			return nil, errors.New("图片尚未上传完成")
		}
		thumbnail, err := os.ReadFile(base + ".thumb.jpg")
		if err != nil {
			return nil, err
		}
		return map[string]any{"id": record.ID, "name": record.Name, "bytes": base64.RawURLEncoding.EncodeToString(thumbnail)}, nil
	}
	if record.ClientID != command.clientID {
		return nil, errors.New("图片不属于当前手机")
	}
	switch command.Action {
	case "image.chunk":
		chunk, err := base64.RawURLEncoding.DecodeString(data.Bytes)
		if err != nil || len(chunk) == 0 || len(chunk) > imageChunkBytes || data.Offset < 0 || data.Offset+int64(len(chunk)) > record.Size {
			return nil, errors.New("图片数据块无效")
		}
		path := base + ".part"
		if record.Complete {
			path = base + imageExtension(record.MimeType)
		}
		file, err := os.OpenFile(path, os.O_RDWR, 0o600)
		if err != nil {
			return nil, err
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil {
			return nil, err
		}
		if data.Offset < info.Size() && data.Offset+int64(len(chunk)) <= info.Size() {
			existing := make([]byte, len(chunk))
			if _, err := file.ReadAt(existing, data.Offset); err != nil || !bytes.Equal(existing, chunk) {
				return nil, errors.New("重试的图片数据与已上传内容不一致")
			}
			return map[string]any{"offset": info.Size()}, nil
		}
		if record.Complete || data.Offset != info.Size() {
			return nil, errors.New("图片上传顺序不匹配，请重试上传")
		}
		if _, err := file.WriteAt(chunk, data.Offset); err != nil {
			return nil, err
		}
		return map[string]any{"offset": data.Offset + int64(len(chunk))}, nil
	case "image.finish":
		if record.Complete {
			return map[string]any{"id": record.ID}, nil
		}
		raw, err := os.ReadFile(base + ".part")
		if err != nil {
			return nil, err
		}
		digest := sha256.Sum256(raw)
		if int64(len(raw)) != record.Size || hex.EncodeToString(digest[:]) != record.SHA256 {
			return nil, errors.New("图片上传不完整或校验失败，请重新选择图片")
		}
		config, format, err := image.DecodeConfig(bytes.NewReader(raw))
		if err != nil || config.Width <= 0 || config.Height <= 0 || config.Width > 8192 || config.Height > 8192 || int64(config.Width)*int64(config.Height) > 32_000_000 || (record.MimeType == "image/png" && format != "png") || (record.MimeType == "image/jpeg" && format != "jpeg") {
			return nil, errors.New("图片格式或尺寸无效")
		}
		decoded, _, err := image.Decode(bytes.NewReader(raw))
		if err != nil {
			return nil, errors.New("图片无法解码，请重新选择图片")
		}
		var thumbnail bytes.Buffer
		if err := jpeg.Encode(&thumbnail, imageThumbnail(decoded), &jpeg.Options{Quality: 80}); err != nil {
			return nil, err
		}
		if err := os.WriteFile(base+".thumb.jpg", thumbnail.Bytes(), 0o600); err != nil {
			return nil, err
		}
		if err := os.Rename(base+".part", base+imageExtension(record.MimeType)); err != nil {
			return nil, err
		}
		record.Complete = true
		if err := saveImageRecord(base, record); err != nil {
			return nil, err
		}
		return map[string]any{"id": record.ID}, nil
	default:
		return nil, fmt.Errorf("unsupported image action %q", command.Action)
	}
}

func imageThumbnail(source image.Image) image.Image {
	bounds := source.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w > 512 || h > 512 {
		if w >= h {
			h, w = max(1, h*512/w), 512
		} else {
			w, h = max(1, w*512/h), 512
		}
	}
	thumb := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, b, a := source.At(bounds.Min.X+x*bounds.Dx()/w, bounds.Min.Y+y*bounds.Dy()/h).RGBA()
			// Composite transparency onto white before encoding JPEG.
			thumb.SetRGBA(x, y, color.RGBA{uint8((r + 65535 - a) >> 8), uint8((g + 65535 - a) >> 8), uint8((b + 65535 - a) >> 8), 255})
		}
	}
	return thumb
}

func (r *Runtime) imagePaths(threadID, clientID string, ids []string) ([]string, error) {
	if len(ids) > maxImagesPerMessage {
		return nil, errors.New("每条消息最多添加 4 张图片")
	}
	paths := make([]string, 0, len(ids))
	for _, id := range ids {
		base, err := r.imageBase(threadID, id)
		if err != nil {
			return nil, err
		}
		record, err := loadImageRecord(base)
		if err != nil || !record.Complete || record.ThreadID != threadID || record.ClientID != clientID {
			return nil, errors.New("请先完成图片上传，再发送消息")
		}
		path, err := filepath.Abs(base + imageExtension(record.MimeType))
		if err != nil {
			return nil, err
		}
		if _, err := os.Stat(path); err != nil {
			return nil, err
		}
		paths = append(paths, path)
	}
	return paths, nil
}

func (r *Runtime) annotateImages(threadID string, items []any) {
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		if item["type"] != "userMessage" {
			continue
		}
		content, _ := item["content"].([]any)
		for _, raw := range content {
			input, _ := raw.(map[string]any)
			if input["type"] != "localImage" {
				continue
			}
			path, _ := input["path"].(string)
			id := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
			base, err := r.imageBase(threadID, id)
			if err != nil {
				continue
			}
			record, err := loadImageRecord(base)
			if err == nil && record.Complete && record.ThreadID == threadID && strings.EqualFold(filepath.Clean(path), filepath.Clean(base+imageExtension(record.MimeType))) {
				input["attachmentId"], input["name"] = record.ID, record.Name
			}
		}
	}
}

func (r *Runtime) annotateThreadImages(result map[string]any) {
	thread, _ := result["thread"].(map[string]any)
	threadID, _ := thread["id"].(string)
	turns, _ := thread["turns"].([]any)
	for _, raw := range turns {
		turn, _ := raw.(map[string]any)
		items, _ := turn["items"].([]any)
		r.annotateImages(threadID, items)
	}
}
