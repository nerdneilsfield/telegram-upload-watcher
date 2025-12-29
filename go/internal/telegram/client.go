package telegram

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"mime/multipart"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/valyala/fasthttp"
	"github.com/valyala/fasthttp/fasthttpproxy"
)

type Client struct {
	urlPool   *URLPool
	tokenPool *TokenPool
	client    *fasthttp.Client
}

type RetryConfig struct {
	MaxRetries int
	Delay      time.Duration
}

type apiResponse struct {
	Ok          bool   `json:"ok"`
	Description string `json:"description"`
	Parameters  struct {
		RetryAfter int `json:"retry_after"`
	} `json:"parameters"`
}

func NewClient(urlPool *URLPool, tokenPool *TokenPool) *Client {
	client := &fasthttp.Client{}
	if proxy := getProxyFromEnv(); proxy != "" {
		client.Dial = fasthttpproxy.FasthttpHTTPDialerTimeout(proxy, 15*time.Second)
	}
	return &Client{
		urlPool:   urlPool,
		tokenPool: tokenPool,
		client:    client,
	}
}

func getProxyFromEnv() string {
	proxy := os.Getenv("https_proxy")
	if proxy == "" {
		proxy = os.Getenv("HTTPS_PROXY")
	}
	if proxy == "" {
		return ""
	}
	if strings.Contains(proxy, "://") {
		parsed, err := url.Parse(proxy)
		if err == nil && parsed.Host != "" {
			if parsed.User != nil {
				return parsed.User.String() + "@" + parsed.Host
			}
			return parsed.Host
		}
	}
	return proxy
}

func (c *Client) TestToken(apiURL string, token string) bool {
	if apiURL == "" || token == "" {
		return false
	}
	url := apiURL + "/bot" + token + "/getMe"
	req := fasthttp.AcquireRequest()
	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseRequest(req)
	defer fasthttp.ReleaseResponse(resp)

	req.SetRequestURI(url)
	req.Header.SetMethod("GET")
	if err := c.client.Do(req, resp); err != nil {
		return false
	}
	var parsed apiResponse
	if err := json.Unmarshal(resp.Body(), &parsed); err != nil {
		return false
	}
	return parsed.Ok
}

func (c *Client) SendMessage(chatID string, text string, topicID *int, retry RetryConfig) error {
	form := url.Values{}
	form.Set("chat_id", chatID)
	form.Set("text", text)
	if topicID != nil {
		form.Set("message_thread_id", fmt.Sprintf("%d", *topicID))
	}
	return c.doRequest("/sendMessage", []byte(form.Encode()), "application/x-www-form-urlencoded", retry)
}

type MediaFile struct {
	Filename string
	Data     []byte
}

func (c *Client) SendMediaGroup(chatID string, media []MediaFile, topicID *int, retry RetryConfig) error {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	writer.WriteField("chat_id", chatID)
	if topicID != nil {
		writer.WriteField("message_thread_id", fmt.Sprintf("%d", *topicID))
	}

	mediaItems := []map[string]string{}
	for idx, file := range media {
		field := fmt.Sprintf("file%d", idx)
		part, err := writer.CreateFormFile(field, file.Filename)
		if err != nil {
			return err
		}
		if _, err := part.Write(file.Data); err != nil {
			return err
		}
		mediaItems = append(mediaItems, map[string]string{
			"type":  "photo",
			"media": "attach://" + field,
		})
	}

	payload, err := json.Marshal(mediaItems)
	if err != nil {
		return err
	}
	writer.WriteField("media", string(payload))
	writer.Close()

	return c.doRequest("/sendMediaGroup", body.Bytes(), writer.FormDataContentType(), retry)
}

func (c *Client) SendDocument(chatID string, file MediaFile, topicID *int, retry RetryConfig) error {
	return c.sendFile("/sendDocument", "document", chatID, file, topicID, retry)
}

func (c *Client) SendVideo(chatID string, file MediaFile, topicID *int, retry RetryConfig) error {
	return c.sendFile("/sendVideo", "video", chatID, file, topicID, retry)
}

func (c *Client) SendAudio(chatID string, file MediaFile, topicID *int, retry RetryConfig) error {
	return c.sendFile("/sendAudio", "audio", chatID, file, topicID, retry)
}

func (c *Client) sendFile(path string, fieldName string, chatID string, file MediaFile, topicID *int, retry RetryConfig) error {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	writer.WriteField("chat_id", chatID)
	if topicID != nil {
		writer.WriteField("message_thread_id", fmt.Sprintf("%d", *topicID))
	}

	part, err := writer.CreateFormFile(fieldName, file.Filename)
	if err != nil {
		return err
	}
	if _, err := part.Write(file.Data); err != nil {
		return err
	}
	writer.Close()

	return c.doRequest(path, body.Bytes(), writer.FormDataContentType(), retry)
}

func (c *Client) doRequest(path string, body []byte, contentType string, retry RetryConfig) error {
	if retry.MaxRetries <= 0 {
		retry.MaxRetries = 1
	}
	for attempt := 1; attempt <= retry.MaxRetries; attempt++ {
		err := c.doRequestOnce(path, body, contentType)
		if err == nil {
			return nil
		}
		if attempt == retry.MaxRetries {
			return err
		}
		time.Sleep(retry.Delay)
	}
	return nil
}

func (c *Client) doRequestOnce(path string, body []byte, contentType string) error {
	apiURL := c.urlPool.Get()
	token := c.tokenPool.Get()
	if apiURL == "" || token == "" {
		return fmt.Errorf("no available api url or token")
	}
	defer c.urlPool.Increment(apiURL)

	url := apiURL + "/bot" + token + path
	req := fasthttp.AcquireRequest()
	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseRequest(req)
	defer fasthttp.ReleaseResponse(resp)

	req.SetRequestURI(url)
	req.Header.SetMethod("POST")
	req.Header.SetContentType(contentType)
	req.SetBodyRaw(body)

	if err := c.client.Do(req, resp); err != nil {
		return err
	}

	var parsed apiResponse
	if err := json.Unmarshal(resp.Body(), &parsed); err != nil {
		return err
	}
	if parsed.Ok {
		c.tokenPool.Increment(token)
		return nil
	}
	if parsed.Parameters.RetryAfter > 0 {
		time.Sleep(time.Duration(parsed.Parameters.RetryAfter) * time.Second)
	}
	if parsed.Description != "" {
		log.Printf("telegram error: %s", parsed.Description)
	}
	c.tokenPool.Remove(token)
	return fmt.Errorf("telegram request failed: %s", parsed.Description)
}
