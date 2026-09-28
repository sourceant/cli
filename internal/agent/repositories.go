package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
)

func (c *Client) Register(ctx context.Context, path, name string) (Repository, error) {
	var repository Repository
	body, err := json.Marshal(map[string]string{"path": path, "name": name})
	if err != nil {
		return repository, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/repositories", bytes.NewReader(body))
	if err != nil {
		return repository, err
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(req)
	if err != nil {
		return repository, &Unreachable{BaseURL: c.baseURL, Cause: err}
	}
	defer func() { _ = response.Body.Close() }()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		return repository, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return repository, &Error{StatusCode: response.StatusCode, Detail: detail(data)}
	}
	err = json.Unmarshal(data, &repository)
	return repository, err
}
