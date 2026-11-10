package macos

import (
	"encoding/xml"
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLaunchAgentEscapesPathsAndPreservesArgumentBoundaries(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "dcode & 中文", "codex-remote")
	job := LaunchAgent{Label: AgentLabel, Arguments: []string{executable, "run"}, Path: "/custom<&>/bin:/usr/bin", LogPath: filepath.Join(t.TempDir(), "agent.log")}
	raw, err := job.XML()
	if err != nil {
		t.Fatal(err)
	}
	decoder := xml.NewDecoder(strings.NewReader(string(raw)))
	for {
		_, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	var decoded struct {
		Arguments []string `xml:"dict>array>string"`
	}
	if err := xml.Unmarshal(raw, &decoded); err != nil || !reflect.DeepEqual(decoded.Arguments, job.Arguments) {
		t.Fatalf("argument boundaries changed: %q, %v", decoded.Arguments, err)
	}
	if !strings.Contains(string(raw), "<key>RunAtLoad</key>\n<true/>") || strings.Contains(string(raw), "<key>KeepAlive</key>") {
		t.Fatal("login job must start once per login without a restart loop")
	}
	if strings.Contains(string(raw), job.Path) || !strings.Contains(string(raw), "&lt;&amp;&gt;") {
		t.Fatal("environment path was not XML escaped")
	}
}

func TestMacPathAddsAppleSiliconHomebrewWithoutEmptyOrDuplicateEntries(t *testing.T) {
	path := SearchPath("/custom/bin::/usr/bin:/custom/bin", "/Users/person")
	if !strings.HasPrefix(path, "/custom/bin:/usr/bin:") || !strings.Contains(path, "/opt/homebrew/bin") || !strings.Contains(path, "/Users/person/.local/bin") || strings.Contains(path, "::") || strings.Count(path, "/custom/bin") != 1 {
		t.Fatalf("unexpected search path %q", path)
	}
}
