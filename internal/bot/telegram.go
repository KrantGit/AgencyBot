package bot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

type TelegramClient struct {
	baseURL string
	client  *http.Client
}

type Update struct {
	ID      int64 `json:"update_id"`
	Message *struct {
		Text string `json:"text"`
		Chat struct {
			ID int64 `json:"id"`
		} `json:"chat"`
		From struct {
			ID int64 `json:"id"`
		} `json:"from"`
	} `json:"message"`
}

type telegramResponse[T any] struct {
	OK          bool   `json:"ok"`
	Description string `json:"description"`
	Result      T      `json:"result"`
}

func NewTelegramClient(token string) *TelegramClient {
	return &TelegramClient{baseURL: "https://api.telegram.org/bot" + token + "/", client: http.DefaultClient}
}

func (c *TelegramClient) Updates(ctx context.Context, offset int64) ([]Update, error) {
	query := url.Values{"timeout": {"30"}}
	if offset > 0 {
		query.Set("offset", strconv.FormatInt(offset, 10))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"getUpdates?"+query.Encode(), nil)
	if err != nil {
		return nil, err
	}
	var response telegramResponse[[]Update]
	if err = c.do(req, &response); err != nil {
		return nil, err
	}
	if !response.OK {
		return nil, fmt.Errorf("telegram getUpdates failed: %s", response.Description)
	}
	return response.Result, nil
}

func (c *TelegramClient) Send(ctx context.Context, chatID int64, text string) error {
	body, err := json.Marshal(map[string]any{"chat_id": chatID, "text": text})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"sendMessage", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	var response telegramResponse[json.RawMessage]
	if err := c.do(req, &response); err != nil {
		return err
	}
	if !response.OK {
		return fmt.Errorf("telegram sendMessage failed: %s", response.Description)
	}
	return nil
}

func (c *TelegramClient) do(req *http.Request, result any) error {
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if err = json.NewDecoder(resp.Body).Decode(result); err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("telegram returned HTTP %d", resp.StatusCode)
	}
	return nil
}
