package agent

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"codexremote/internal/protocol"
)

func (r *Runtime) prepareLinkPairing() error {
	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		return err
	}
	config, err := r.store.LoadConfig()
	if err != nil {
		return err
	}
	config.Pairing = &Pairing{Code: base64.RawURLEncoding.EncodeToString(token), ExpiresAt: time.Now().Add(10 * time.Minute), AutoApprove: true}
	config.Pending = map[string]PendingClient{}
	if err := r.store.Save(config); err != nil {
		return err
	}
	r.setConfig(config)
	return nil
}

func relayPairingURL(relayURL string) (string, error) {
	endpoint, err := url.Parse(relayURL)
	if err != nil {
		return "", err
	}
	switch endpoint.Scheme {
	case "ws":
		endpoint.Scheme = "http"
	case "wss":
		endpoint.Scheme = "https"
	default:
		return "", errors.New("invalid Relay URL")
	}
	endpoint.Path = strings.TrimSuffix(endpoint.Path, "/ws") + "/pairing/register"
	endpoint.RawQuery, endpoint.Fragment = "", ""
	return endpoint.String(), nil
}

func (r *Runtime) registerLinkPairing(ctx context.Context) error {
	config, err := r.store.LoadConfig()
	if err != nil {
		return err
	}
	if config.Pairing == nil || !config.Pairing.AutoApprove || !time.Now().Before(config.Pairing.ExpiresAt) {
		return nil
	}
	endpoint, err := relayPairingURL(config.RelayURL)
	if err != nil {
		return err
	}
	// HTTP registration has its own signature sequence, independent of socket events.
	signer, err := protocol.NewSigner(config.HostID, r.private)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 5 * time.Second}
	for attempt := 0; attempt < 20; attempt++ {
		envelope, err := signer.Sign("relay", "pair.register", map[string]any{"token": config.Pairing.Code, "expiresAt": config.Pairing.ExpiresAt.UnixMilli()})
		if err != nil {
			return err
		}
		raw, err := json.Marshal(envelope)
		if err != nil {
			return err
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
		if err != nil {
			return err
		}
		request.Header.Set("Content-Type", "application/json")
		response, err := client.Do(request)
		if err != nil {
			return err
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, 8192))
		response.Body.Close()
		if readErr != nil {
			return readErr
		}
		if response.StatusCode == http.StatusNotFound && strings.Contains(string(body), "host_offline") {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(50 * time.Millisecond):
				continue
			}
		}
		if response.StatusCode == http.StatusNotFound {
			return errors.New("中转服务器尚未支持六位连接码，请更新 Relay；原扫码配对仍可使用")
		}
		if response.StatusCode != http.StatusOK {
			return fmt.Errorf("register connection code: HTTP %d", response.StatusCode)
		}
		var result struct {
			Code string `json:"code"`
		}
		if json.Unmarshal(body, &result) != nil || len(result.Code) != 6 || strings.Trim(result.Code, "0123456789") != "" {
			return errors.New("invalid connection code from Relay")
		}
		fmt.Printf("\n手机连接码：%s\n在 dcode 中输入这 6 位数字即可连接，有效期至 %s，成功配对后作废。\n已配对手机无需再次输入。\n\n", result.Code, config.Pairing.ExpiresAt.Local().Format("15:04:05"))
		return nil
	}
	return errors.New("Relay has not registered the Agent connection")
}
