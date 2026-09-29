package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// DefaultAPIURL is the base URL of Telegram's hosted Bot API.
const DefaultAPIURL = "https://api.telegram.org"

// Update is an incoming Bot API update. Message is nil for update kinds other than a new message.
type Update struct {
	UpdateID int64    `json:"update_id"`
	Message  *Message `json:"message,omitempty"`
}

// Message is a message sent to the bot.
type Message struct {
	MessageID int64  `json:"message_id"`
	From      *User  `json:"from,omitempty"`
	Chat      Chat   `json:"chat"`
	Text      string `json:"text,omitempty"`
}

// Chat is the conversation a message belongs to. A private chat's ID is the Telegram user ID of the
// person talking to the bot.
type Chat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}

// User is a Telegram user or bot.
type User struct {
	ID       int64  `json:"id"`
	IsBot    bool   `json:"is_bot"`
	Username string `json:"username,omitempty"`
}

// APIError is an error Telegram returned for a Bot API request.
type APIError struct {
	Method      string
	Code        int
	Description string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("telegram %s: %d %s", e.Method, e.Code, e.Description)
}

// Client calls the Telegram Bot API with a bot token. It implements Sender.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// NewClient returns a client for the Bot API at baseURL, usually DefaultAPIURL, authenticating
// with token. The client's timeout, if any, must exceed the long-polling timeout given to
// GetUpdates.
func NewClient(baseURL, token string, httpClient *http.Client) *Client {
	return &Client{baseURL: baseURL, token: token, httpClient: httpClient}
}

// GetMe returns the bot the token belongs to, confirming the token is valid.
func (c *Client) GetMe(ctx context.Context) (User, error) {
	var bot User
	err := c.call(ctx, "getMe", nil, &bot)
	return bot, err
}

// GetUpdates long polls for message updates with an ID of at least offset, waiting up to timeout
// for one to arrive. Passing an offset confirms every earlier update, which Telegram then forgets.
func (c *Client) GetUpdates(ctx context.Context, offset int64, timeout time.Duration) ([]Update, error) {
	params := struct {
		Offset         int64    `json:"offset"`
		Timeout        int      `json:"timeout"`
		AllowedUpdates []string `json:"allowed_updates"`
	}{offset, int(timeout / time.Second), []string{"message"}}
	var updates []Update
	err := c.call(ctx, "getUpdates", params, &updates)
	return updates, err
}

// SendMessage sends text to chatID, formatted according to parseMode unless it is empty.
func (c *Client) SendMessage(ctx context.Context, chatID int64, text, parseMode string) error {
	params := struct {
		ChatID    int64  `json:"chat_id"`
		Text      string `json:"text"`
		ParseMode string `json:"parse_mode,omitempty"`
	}{chatID, text, parseMode}
	return c.call(ctx, "sendMessage", params, nil)
}

// call invokes method with params encoded as JSON, decoding a successful result into result unless
// it is nil. Errors never include the request URL, because it contains the bot token.
func (c *Client) call(ctx context.Context, method string, params, result any) error {
	var body bytes.Buffer
	if params != nil {
		if err := json.NewEncoder(&body).Encode(params); err != nil {
			return fmt.Errorf("telegram %s: encode request: %w", method, err)
		}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/bot"+c.token+"/"+method, &body)
	if err != nil {
		return fmt.Errorf("telegram %s: build request: %w", method, withoutURL(err))
	}
	if params != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("telegram %s: %w", method, withoutURL(err))
	}
	defer response.Body.Close()

	var envelope struct {
		OK          bool            `json:"ok"`
		Result      json.RawMessage `json:"result"`
		ErrorCode   int             `json:"error_code"`
		Description string          `json:"description"`
	}
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		return fmt.Errorf("telegram %s: decode response (HTTP %d): %w", method, response.StatusCode, err)
	}
	if !envelope.OK {
		code := envelope.ErrorCode
		if code == 0 {
			code = response.StatusCode
		}
		return &APIError{Method: method, Code: code, Description: envelope.Description}
	}
	if result != nil {
		if err := json.Unmarshal(envelope.Result, result); err != nil {
			return fmt.Errorf("telegram %s: decode result: %w", method, err)
		}
	}
	return nil
}

// withoutURL strips the request URL, which contains the bot token, from an HTTP client error.
func withoutURL(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return urlErr.Err
	}
	return err
}
