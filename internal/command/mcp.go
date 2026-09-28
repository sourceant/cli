package command

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/spf13/cobra"
)

func mcpCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use: "mcp", Short: "Connect an MCP client over standard input and output", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			address := opts.agentURL
			if !cmd.Flags().Changed("agent") && os.Getenv(EnvAgent) == "" && os.Getenv("SOURCEANT_UI_URL") != "" {
				address = os.Getenv("SOURCEANT_UI_URL")
			}
			bridge := &mcpBridge{
				endpoint: strings.TrimRight(address, "/") + "/mcp/",
				client:   &http.Client{Timeout: opts.timeout},
				output:   cmd.OutOrStdout(),
			}
			return bridge.run(cmd.Context(), cmd.InOrStdin(), cmd.ErrOrStderr())
		},
	}
}

type rpcMessage struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Result struct {
		ProtocolVersion string `json:"protocolVersion"`
	} `json:"result,omitempty"`
}

type mcpBridge struct {
	endpoint string
	client   *http.Client
	output   io.Writer
	mu       sync.Mutex
	session  string
	version  string
}

const maxMCPMessage = 16 << 20

func (b *mcpBridge) emit(data []byte) error {
	var compact bytes.Buffer
	if err := json.Compact(&compact, data); err != nil {
		return fmt.Errorf("the MCP server returned an invalid message: %w", err)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	_, err := fmt.Fprintln(b.output, compact.String())
	return err
}

func (b *mcpBridge) forward(ctx context.Context, data []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.endpoint, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	b.mu.Lock()
	if b.session != "" {
		req.Header.Set("Mcp-Session-Id", b.session)
	}
	if b.version != "" {
		req.Header.Set("MCP-Protocol-Version", b.version)
	}
	b.mu.Unlock()
	response, err := b.client.Do(req)
	if err != nil {
		return fmt.Errorf("cannot reach the agent MCP endpoint: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("agent MCP endpoint returned HTTP %d", response.StatusCode)
	}
	if response.StatusCode == http.StatusAccepted || response.StatusCode == http.StatusNoContent {
		return nil
	}
	b.mu.Lock()
	if session := response.Header.Get("Mcp-Session-Id"); session != "" {
		b.session = session
	}
	b.mu.Unlock()
	emit := func(raw []byte) error {
		var message rpcMessage
		if err := json.Unmarshal(raw, &message); err != nil {
			return err
		}
		if message.Result.ProtocolVersion != "" {
			b.mu.Lock()
			b.version = message.Result.ProtocolVersion
			b.mu.Unlock()
		}
		return b.emit(raw)
	}
	contentType, _, _ := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if contentType == "application/json" {
		raw, err := io.ReadAll(io.LimitReader(response.Body, maxMCPMessage+1))
		if err != nil {
			return err
		}
		if len(raw) > maxMCPMessage {
			return fmt.Errorf("MCP response exceeds 16 MiB")
		}
		return emit(raw)
	}
	if contentType != "text/event-stream" {
		return fmt.Errorf("unexpected MCP response type %q", contentType)
	}
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 4096), maxMCPMessage)
	var event strings.Builder
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" && event.Len() > 0 {
			if err := emit([]byte(event.String())); err != nil {
				return err
			}
			event.Reset()
		} else if strings.HasPrefix(line, "data:") {
			if event.Len() > 0 {
				event.WriteByte('\n')
			}
			event.WriteString(strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
			if event.Len() > maxMCPMessage {
				return fmt.Errorf("MCP event exceeds 16 MiB")
			}
		}
	}
	return scanner.Err()
}

func (b *mcpBridge) run(ctx context.Context, input io.Reader, stderr io.Writer) error {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 4096), maxMCPMessage)
	var pending sync.WaitGroup
	defer pending.Wait()
	for scanner.Scan() {
		data := bytes.Clone(scanner.Bytes())
		if len(bytes.TrimSpace(data)) == 0 {
			continue
		}
		var message rpcMessage
		if err := json.Unmarshal(data, &message); err != nil {
			return fmt.Errorf("invalid MCP input: %w", err)
		}
		if message.Method == "initialize" || len(message.ID) == 0 {
			if err := b.forward(ctx, data); err != nil {
				return err
			}
			continue
		}
		pending.Add(1)
		go func() {
			defer pending.Done()
			if err := b.forward(ctx, data); err != nil {
				reply, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": message.ID, "error": map[string]any{"code": -32000, "message": err.Error()}})
				if writeErr := b.emit(reply); writeErr != nil {
					b.mu.Lock()
					_, _ = fmt.Fprintln(stderr, writeErr)
					b.mu.Unlock()
				}
			}
		}()
	}
	return scanner.Err()
}
