package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
)

func (c *Client) Architecture(ctx context.Context, repository string, depth int, includeTests bool) (json.RawMessage, error) {
	return get[json.RawMessage](ctx, c, "/api/architecture", url.Values{
		"repository": {repository}, "depth": {strconv.Itoa(depth)}, "include_tests": {strconv.FormatBool(includeTests)},
	})
}

func (c *Client) CompareArchitecture(ctx context.Context, baseline json.RawMessage) (json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/architecture/compare", bytes.NewReader(baseline))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, &Unreachable{BaseURL: c.baseURL, Cause: err}
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, &Error{StatusCode: resp.StatusCode, Detail: detail(body)}
	}
	if !json.Valid(body) {
		return nil, fmt.Errorf("the agent returned an invalid architecture comparison")
	}
	return json.RawMessage(body), nil
}
