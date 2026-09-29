package telegram_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/griffdawg123/meal-planner/telegram"
)

const testToken = "123456:secret-test-token"

func TestClient(t *testing.T) {
	t.Run("sends a message to the chat with the given text and parse mode", func(t *testing.T) {
		api := newFakeBotAPI(t, `{"ok":true,"result":{"message_id":1}}`)
		client := telegram.NewClient(api.server.URL, testToken, api.server.Client())

		if err := client.SendMessage(context.Background(), 1001, "hello", telegram.ParseMode); err != nil {
			t.Fatalf("send message: got %v, want nil", err)
		}

		if api.path != "/bot"+testToken+"/sendMessage" {
			t.Errorf("request path: got %q, want %q", api.path, "/bot"+testToken+"/sendMessage")
		}
		want := map[string]any{"chat_id": float64(1001), "text": "hello", "parse_mode": "HTML"}
		if !reflect.DeepEqual(api.body, want) {
			t.Errorf("request body: got %v, want %v", api.body, want)
		}
	})

	t.Run("omits the parse mode when none is given", func(t *testing.T) {
		api := newFakeBotAPI(t, `{"ok":true,"result":{"message_id":1}}`)
		client := telegram.NewClient(api.server.URL, testToken, api.server.Client())

		if err := client.SendMessage(context.Background(), 1001, "hello", ""); err != nil {
			t.Fatalf("send message: got %v, want nil", err)
		}

		if _, ok := api.body["parse_mode"]; ok {
			t.Errorf("request body: got parse_mode %v, want none", api.body["parse_mode"])
		}
	})

	t.Run("long polls for message updates after the given offset", func(t *testing.T) {
		api := newFakeBotAPI(t, `{"ok":true,"result":[
			{"update_id":7,"message":{"message_id":3,"from":{"id":1001,"is_bot":false,"username":"sam"},"chat":{"id":1001,"type":"private"},"text":"/ping"}},
			{"update_id":8}
		]}`)
		client := telegram.NewClient(api.server.URL, testToken, api.server.Client())

		updates, err := client.GetUpdates(context.Background(), 7, 30*time.Second)
		if err != nil {
			t.Fatalf("get updates: got %v, want nil", err)
		}

		if api.path != "/bot"+testToken+"/getUpdates" {
			t.Errorf("request path: got %q, want %q", api.path, "/bot"+testToken+"/getUpdates")
		}
		wantBody := map[string]any{"offset": float64(7), "timeout": float64(30), "allowed_updates": []any{"message"}}
		if !reflect.DeepEqual(api.body, wantBody) {
			t.Errorf("request body: got %v, want %v", api.body, wantBody)
		}
		want := []telegram.Update{
			{UpdateID: 7, Message: &telegram.Message{
				MessageID: 3,
				From:      &telegram.User{ID: 1001, Username: "sam"},
				Chat:      telegram.Chat{ID: 1001, Type: "private"},
				Text:      "/ping",
			}},
			{UpdateID: 8},
		}
		if !reflect.DeepEqual(updates, want) {
			t.Fatalf("updates: got %+v, want %+v", updates, want)
		}
	})

	t.Run("identifies the bot the token belongs to", func(t *testing.T) {
		api := newFakeBotAPI(t, `{"ok":true,"result":{"id":42,"is_bot":true,"username":"meal_planner_bot"}}`)
		client := telegram.NewClient(api.server.URL, testToken, api.server.Client())

		bot, err := client.GetMe(context.Background())
		if err != nil {
			t.Fatalf("get me: got %v, want nil", err)
		}

		if api.path != "/bot"+testToken+"/getMe" {
			t.Errorf("request path: got %q, want %q", api.path, "/bot"+testToken+"/getMe")
		}
		want := telegram.User{ID: 42, IsBot: true, Username: "meal_planner_bot"}
		if bot != want {
			t.Fatalf("bot: got %+v, want %+v", bot, want)
		}
	})

	t.Run("identifies a token Telegram rejects when getting the bot", func(t *testing.T) {
		for _, response := range []string{
			`{"ok":false,"error_code":401,"description":"Unauthorized"}`,
			`{"ok":false,"error_code":404,"description":"Not Found"}`,
		} {
			api := newFakeBotAPI(t, response)
			client := telegram.NewClient(api.server.URL, testToken, api.server.Client())

			_, err := client.GetMe(context.Background())

			var apiErr *telegram.APIError
			if !errors.Is(err, telegram.ErrBotTokenRejected) || !errors.As(err, &apiErr) {
				t.Fatalf("get me with response %s: got %v, want it to wrap %v and an *telegram.APIError", response, err, telegram.ErrBotTokenRejected)
			}
			if strings.Contains(err.Error(), testToken) {
				t.Fatalf("error: got %q, want it not to contain the bot token", err)
			}
		}
	})

	t.Run("does not blame the token for other failures getting the bot", func(t *testing.T) {
		api := newFakeBotAPI(t, `{"ok":false,"error_code":500,"description":"Internal Server Error"}`)
		client := telegram.NewClient(api.server.URL, testToken, api.server.Client())

		_, err := client.GetMe(context.Background())

		if err == nil || errors.Is(err, telegram.ErrBotTokenRejected) {
			t.Fatalf("get me: got %v, want an error that does not wrap %v", err, telegram.ErrBotTokenRejected)
		}
	})

	t.Run("deletes any webhook so updates can be long polled, keeping pending updates", func(t *testing.T) {
		api := newFakeBotAPI(t, `{"ok":true,"result":true}`)
		client := telegram.NewClient(api.server.URL, testToken, api.server.Client())

		if err := client.DeleteWebhook(context.Background()); err != nil {
			t.Fatalf("delete webhook: got %v, want nil", err)
		}

		if api.path != "/bot"+testToken+"/deleteWebhook" {
			t.Errorf("request path: got %q, want %q", api.path, "/bot"+testToken+"/deleteWebhook")
		}
		want := map[string]any{"drop_pending_updates": false}
		if !reflect.DeepEqual(api.body, want) {
			t.Errorf("request body: got %v, want %v", api.body, want)
		}
	})

	t.Run("reports an error Telegram returns as an APIError", func(t *testing.T) {
		api := newFakeBotAPI(t, `{"ok":false,"error_code":403,"description":"Forbidden: bot was blocked by the user"}`)
		client := telegram.NewClient(api.server.URL, testToken, api.server.Client())

		err := client.SendMessage(context.Background(), 1001, "hello", "")

		var apiErr *telegram.APIError
		if !errors.As(err, &apiErr) {
			t.Fatalf("error: got %v, want an *telegram.APIError", err)
		}
		want := telegram.APIError{Method: "sendMessage", Code: 403, Description: "Forbidden: bot was blocked by the user"}
		if *apiErr != want {
			t.Fatalf("api error: got %+v, want %+v", *apiErr, want)
		}
	})

	t.Run("reports how long to wait when Telegram rate limits a request", func(t *testing.T) {
		api := newFakeBotAPI(t, `{"ok":false,"error_code":429,"description":"Too Many Requests: retry after 3","parameters":{"retry_after":3}}`)
		client := telegram.NewClient(api.server.URL, testToken, api.server.Client())

		err := client.SendMessage(context.Background(), 1001, "hello", "")

		var apiErr *telegram.APIError
		if !errors.As(err, &apiErr) {
			t.Fatalf("error: got %v, want an *telegram.APIError", err)
		}
		want := telegram.APIError{Method: "sendMessage", Code: 429, Description: "Too Many Requests: retry after 3", RetryAfter: 3 * time.Second}
		if *apiErr != want {
			t.Fatalf("api error: got %+v, want %+v", *apiErr, want)
		}
	})

	t.Run("never includes the token in a connection error", func(t *testing.T) {
		api := newFakeBotAPI(t, `{"ok":true}`)
		client := telegram.NewClient(api.server.URL, testToken, api.server.Client())
		api.server.Close()

		err := client.SendMessage(context.Background(), 1001, "hello", "")

		if err == nil {
			t.Fatalf("send message: got nil, want a connection error")
		}
		if strings.Contains(err.Error(), testToken) {
			t.Fatalf("error: got %q, want it not to contain the bot token", err)
		}
	})
}

// fakeBotAPI is a Bot API server that answers every request with response, recording the path and
// JSON body of the last request.
type fakeBotAPI struct {
	server *httptest.Server
	path   string
	body   map[string]any
}

func newFakeBotAPI(t *testing.T, response string) *fakeBotAPI {
	t.Helper()
	api := &fakeBotAPI{}
	api.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		api.path = r.URL.Path
		api.body = nil
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		if len(data) > 0 {
			if err := json.Unmarshal(data, &api.body); err != nil {
				t.Errorf("decode request body %q: %v", data, err)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, response)
	}))
	t.Cleanup(api.server.Close)
	return api
}
