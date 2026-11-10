package macos

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const AgentLabel = "dev.dcode.agent"
const DesktopLabel = "dev.dcode.desktop"

type LaunchAgent struct {
	Label     string
	Arguments []string
	Path      string
	LogPath   string
}

func (job LaunchAgent) XML() ([]byte, error) {
	if (job.Label != AgentLabel && job.Label != DesktopLabel) || len(job.Arguments) == 0 || !filepath.IsAbs(job.Arguments[0]) {
		return nil, errors.New("invalid dcode login startup configuration")
	}
	var out bytes.Buffer
	out.WriteString(xml.Header)
	out.WriteString("<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n<plist version=\"1.0\"><dict>\n")
	value := func(tag, text string) {
		fmt.Fprintf(&out, "<%s>", tag)
		_ = xml.EscapeText(&out, []byte(text))
		fmt.Fprintf(&out, "</%s>\n", tag)
	}
	value("key", "Label")
	value("string", job.Label)
	value("key", "ProgramArguments")
	out.WriteString("<array>\n")
	for _, argument := range job.Arguments {
		value("string", argument)
	}
	out.WriteString("</array>\n")
	value("key", "RunAtLoad")
	out.WriteString("<true/>\n")
	value("key", "LimitLoadToSessionType")
	value("string", "Aqua")
	value("key", "EnvironmentVariables")
	out.WriteString("<dict>\n")
	value("key", "PATH")
	value("string", job.Path)
	out.WriteString("</dict>\n")
	if job.LogPath != "" {
		value("key", "StandardOutPath")
		value("string", job.LogPath)
		value("key", "StandardErrorPath")
		value("string", job.LogPath)
	}
	out.WriteString("</dict></plist>\n")
	return out.Bytes(), nil
}

func LaunchAgentPath(label string) (string, error) {
	if label != AgentLabel && label != DesktopLabel {
		return "", errors.New("invalid dcode launch agent label")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", label+".plist"), nil
}

// Register for the next login. Do not launch another copy of the current app.
func (job LaunchAgent) Install() error {
	raw, err := job.XML()
	if err != nil {
		return err
	}
	path, err := LaunchAgentPath(job.Label)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".dcode-*.plist")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func RemoveLaunchAgent(label string) error {
	path, err := LaunchAgentPath(label)
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
