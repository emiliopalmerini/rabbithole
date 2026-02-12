package rabbitmq

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ManagementClient talks to the RabbitMQ Management HTTP API
type ManagementClient struct {
	baseURL  string
	username string
	password string
	client   *http.Client
}

type Exchange struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	Durable    bool   `json:"durable"`
	AutoDelete bool   `json:"auto_delete"`
	VHost      string `json:"vhost"`
}

type Queue struct {
	Name       string `json:"name"`
	Durable    bool   `json:"durable"`
	AutoDelete bool   `json:"auto_delete"`
	Exclusive  bool   `json:"exclusive"`
	Messages   int    `json:"messages"`
	Consumers  int    `json:"consumers"`
	VHost      string `json:"vhost"`
}

type Binding struct {
	Source          string `json:"source"`
	Destination     string `json:"destination"`
	DestinationType string `json:"destination_type"`
	RoutingKey      string `json:"routing_key"`
	VHost           string `json:"vhost"`
}

// NewManagementClient creates a client from an AMQP URL
// It converts amqp://user:pass@host:5672/ to http://host:15672/api
func NewManagementClient(amqpURL string) (*ManagementClient, error) {
	parsed, err := url.Parse(amqpURL)
	if err != nil {
		return nil, fmt.Errorf("invalid AMQP URL: %w", err)
	}

	username := "guest"
	password := "guest"
	if parsed.User != nil {
		username = parsed.User.Username()
		if p, ok := parsed.User.Password(); ok {
			password = p
		}
	}

	host := parsed.Hostname()
	if host == "" {
		host = "localhost"
	}

	// Management API is typically on port 15672
	baseURL := fmt.Sprintf("http://%s:15672/api", host)

	return &ManagementClient{
		baseURL:  baseURL,
		username: username,
		password: password,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}, nil
}

func (c *ManagementClient) doRequest(ctx context.Context, method, path string, body []byte) (*http.Response, error) {
	reqURL := c.baseURL + path

	var req *http.Request
	var err error
	if body != nil {
		req, err = http.NewRequestWithContext(ctx, method, reqURL, bytes.NewReader(body))
	} else {
		req, err = http.NewRequestWithContext(ctx, method, reqURL, nil)
	}
	if err != nil {
		return nil, err
	}

	req.SetBasicAuth(c.username, c.password)
	req.Header.Set("Content-Type", "application/json")

	return c.client.Do(req)
}

func (c *ManagementClient) GetExchanges(ctx context.Context, vhost string) ([]Exchange, error) {
	path := fmt.Sprintf("/exchanges/%s", url.PathEscape(vhost))
	resp, err := c.doRequest(ctx, "GET", path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	var exchanges []Exchange
	if err := json.NewDecoder(resp.Body).Decode(&exchanges); err != nil {
		return nil, err
	}

	// Filter out default exchanges (those starting with "amq.")
	var filtered []Exchange
	for _, ex := range exchanges {
		if ex.Name != "" && !strings.HasPrefix(ex.Name, "amq.") {
			filtered = append(filtered, ex)
		}
	}

	return filtered, nil
}

func (c *ManagementClient) GetQueues(ctx context.Context, vhost string) ([]Queue, error) {
	path := fmt.Sprintf("/queues/%s", url.PathEscape(vhost))
	resp, err := c.doRequest(ctx, "GET", path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	var queues []Queue
	if err := json.NewDecoder(resp.Body).Decode(&queues); err != nil {
		return nil, err
	}

	return queues, nil
}

func (c *ManagementClient) GetBindings(ctx context.Context, vhost, exchange string) ([]Binding, error) {
	path := fmt.Sprintf("/exchanges/%s/%s/bindings/source",
		url.PathEscape(vhost), url.PathEscape(exchange))
	resp, err := c.doRequest(ctx, "GET", path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	var bindings []Binding
	if err := json.NewDecoder(resp.Body).Decode(&bindings); err != nil {
		return nil, err
	}

	return bindings, nil
}

func (c *ManagementClient) CreateQueue(ctx context.Context, vhost, name string, durable bool) error {
	path := fmt.Sprintf("/queues/%s/%s", url.PathEscape(vhost), url.PathEscape(name))
	body := fmt.Sprintf(`{"durable":%t,"auto_delete":false}`, durable)

	resp, err := c.doRequest(ctx, "PUT", path, []byte(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("failed to create queue: status %d", resp.StatusCode)
	}

	return nil
}

func (c *ManagementClient) CreateBinding(ctx context.Context, vhost, exchange, queue, routingKey string) error {
	path := fmt.Sprintf("/bindings/%s/e/%s/q/%s",
		url.PathEscape(vhost), url.PathEscape(exchange), url.PathEscape(queue))
	body := fmt.Sprintf(`{"routing_key":%q}`, routingKey)

	resp, err := c.doRequest(ctx, "POST", path, []byte(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("failed to create binding: status %d", resp.StatusCode)
	}

	return nil
}

func (c *ManagementClient) DeleteQueue(ctx context.Context, vhost, name string) error {
	path := fmt.Sprintf("/queues/%s/%s", url.PathEscape(vhost), url.PathEscape(name))

	resp, err := c.doRequest(ctx, "DELETE", path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to delete queue: status %d", resp.StatusCode)
	}

	return nil
}
