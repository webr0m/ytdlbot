package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// APIError preserves Telegram's error code and retry hint so callers can
// distinguish a blocked chat (do not retry) from a rate limit (retry).
type APIError struct {
	Code        int
	Description string
	RetryAfter  int
}

func (e *APIError) Error() string {
	if e.Description == "" {
		return fmt.Sprintf("Telegram: error %d", e.Code)
	}
	return "Telegram: " + e.Description
}

type Client struct {
	base string
	http *http.Client
}

func New(base, token string) *Client {
	return &Client{base: strings.TrimRight(base, "/") + "/bot" + token + "/", http: &http.Client{Timeout: 0}}
}
func (c *Client) Call(ctx context.Context, method string, payload any, out any) error {
	body, e := json.Marshal(payload)
	if e != nil {
		return e
	}
	req, e := http.NewRequestWithContext(ctx, "POST", c.base+method, bytes.NewReader(body))
	if e != nil {
		return fmt.Errorf("solicitud Telegram inválida")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, e := c.http.Do(req)
	if e != nil {
		return fmt.Errorf("no se pudo conectar con Telegram")
	}
	defer resp.Body.Close()
	return decode(resp, out)
}
func decode(resp *http.Response, out any) error {
	var result struct {
		OK          bool   `json:"ok"`
		ErrorCode   int    `json:"error_code"`
		Description string `json:"description"`
		Parameters  struct {
			RetryAfter int `json:"retry_after"`
		} `json:"parameters"`
		Result json.RawMessage `json:"result"`
	}
	if e := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&result); e != nil {
		return fmt.Errorf("respuesta Telegram inválida")
	}
	if !result.OK {
		return &APIError{Code: result.ErrorCode, Description: result.Description, RetryAfter: result.Parameters.RetryAfter}
	}
	if out != nil {
		return json.Unmarshal(result.Result, out)
	}
	return nil
}
func (c *Client) Message(ctx context.Context, chat int64, text string, markup any) error {
	p := map[string]any{"chat_id": chat, "text": text, "link_preview_options": map[string]bool{"is_disabled": true}}
	if markup != nil {
		p["reply_markup"] = markup
	}
	var last error
	for attempt := 0; attempt < 3; attempt++ {
		msgCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		last = c.Call(msgCtx, "sendMessage", p, nil)
		cancel()
		if last == nil {
			return nil
		}
		var apiErr *APIError
		if !errors.As(last, &apiErr) || (apiErr.Code != 429 && apiErr.Code < 500) {
			return last
		}
		wait := time.Duration(apiErr.RetryAfter) * time.Second
		if wait <= 0 {
			wait = time.Duration(1<<attempt) * time.Second
		}
		if wait > 10*time.Second {
			wait = 10 * time.Second
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
	return last
}

type SentFile struct {
	FileID string `json:"file_id"`
}
type SentMessage struct {
	Document *SentFile `json:"document"`
	Video    *SentFile `json:"video"`
	Audio    *SentFile `json:"audio"`
}

func (c *Client) Upload(ctx context.Context, chat int64, path, kind string) (string, error) {
	return c.UploadWithProgress(ctx, chat, path, kind, nil)
}

type progressReader struct {
	source      io.Reader
	done, total int64
	last        time.Time
	update      func(int64, int64)
}

func (r *progressReader) Read(p []byte) (int, error) {
	n, e := r.source.Read(p)
	r.done += int64(n)
	if r.update != nil && (time.Since(r.last) > time.Second || r.done == r.total) {
		r.update(r.done, r.total)
		r.last = time.Now()
	}
	return n, e
}
func (c *Client) UploadWithProgress(ctx context.Context, chat int64, path, kind string, update func(int64, int64)) (string, error) {
	f, e := os.Open(path)
	if e != nil {
		return "", e
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil {
		return "", e
	}
	return c.UploadReader(ctx, chat, filepath.Base(path), kind, &progressReader{source: f, total: info.Size(), update: update})
}
func (c *Client) UploadReader(ctx context.Context, chat int64, name, kind string, source io.Reader) (string, error) {
	reader, writer := io.Pipe()
	form := multipart.NewWriter(writer)
	req, e := http.NewRequestWithContext(ctx, "POST", c.base+"send"+strings.ToUpper(kind[:1])+kind[1:], reader)
	if e != nil {
		return "", fmt.Errorf("solicitud inválida")
	}
	req.Header.Set("Content-Type", form.FormDataContentType())
	done := make(chan error, 1)
	go func() {
		e := form.WriteField("chat_id", fmt.Sprint(chat))
		if e == nil && kind == "video" {
			e = form.WriteField("supports_streaming", "true")
		}
		if e == nil {
			var part io.Writer
			part, e = form.CreateFormFile(kind, filepath.Base(name))
			if e == nil {
				_, e = io.Copy(part, source)
			}
		}
		if e == nil {
			e = form.Close()
		}
		writer.CloseWithError(e)
		done <- e
	}()
	resp, e := c.http.Do(req)
	reader.Close()
	writeErr := <-done
	if e != nil {
		return "", fmt.Errorf("falló la subida a Telegram")
	}
	defer resp.Body.Close()
	if writeErr != nil {
		return "", fmt.Errorf("no se pudo enviar el archivo")
	}
	var msg SentMessage
	if e = decode(resp, &msg); e != nil {
		return "", e
	}
	for _, v := range []*SentFile{msg.Document, msg.Video, msg.Audio} {
		if v != nil && v.FileID != "" {
			return v.FileID, nil
		}
	}
	return "", fmt.Errorf("Telegram no confirmó el archivo enviado; revisa el chat antes de reintentar")
}

type Payment struct {
	Currency  string `json:"currency"`
	Amount    int    `json:"total_amount"`
	Payload   string `json:"invoice_payload"`
	ChargeID  string `json:"telegram_payment_charge_id"`
	Expires   int64  `json:"subscription_expiration_date"`
	Recurring bool   `json:"is_recurring"`
}
type Message struct {
	Chat struct {
		ID   int64  `json:"id"`
		Type string `json:"type"`
	} `json:"chat"`
	From       User        `json:"from"`
	Text       string      `json:"text"`
	Payment    *Payment    `json:"successful_payment"`
	Refund     *Payment    `json:"refunded_payment"`
	ChatShared *ChatShared `json:"chat_shared"`
}
type ChatShared struct {
	RequestID int64  `json:"request_id"`
	ChatID    int64  `json:"chat_id"`
	Title     string `json:"title"`
	Username  string `json:"username"`
}
type PreCheckout struct {
	ID       string `json:"id"`
	From     User   `json:"from"`
	Currency string `json:"currency"`
	Amount   int    `json:"total_amount"`
	Payload  string `json:"invoice_payload"`
}
type Update struct {
	ID          int64        `json:"update_id"`
	Message     *Message     `json:"message"`
	PreCheckout *PreCheckout `json:"pre_checkout_query"`
}
