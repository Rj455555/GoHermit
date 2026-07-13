// Package plugin supervises stdio JSON-RPC plugin processes.
package plugin

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	core "github.com/Rj455555/GoHermit/internal/tool"
	v1 "github.com/Rj455555/GoHermit/protocol/plugin/v1"
)

type Config struct {
	Command         []string
	Directory       string
	MaxMessageBytes int
	DefaultTimeout  time.Duration
}
type Client struct {
	cfg         Config
	cmd         *exec.Cmd
	stdin       io.WriteCloser
	stderr      limited
	sendMu      sync.Mutex
	mu          sync.Mutex
	pending     map[string]chan v1.Response
	next        atomic.Uint64
	done        chan struct{}
	terminal    error
	semaphore   chan struct{}
	initialized bool
}
type limited struct {
	mu  sync.Mutex
	buf bytes.Buffer
	max int
}

func (l *limited) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := len(p)
	if l.buf.Len() < l.max {
		keep := min(len(p), l.max-l.buf.Len())
		_, _ = l.buf.Write(p[:keep])
	}
	return n, nil
}
func (l *limited) String() string { l.mu.Lock(); defer l.mu.Unlock(); return l.buf.String() }

func Start(ctx context.Context, cfg Config) (*Client, error) {
	if len(cfg.Command) == 0 {
		return nil, errors.New("plugin command is empty")
	}
	if cfg.MaxMessageBytes < 1024 {
		cfg.MaxMessageBytes = 4 << 20
	}
	if cfg.DefaultTimeout <= 0 {
		cfg.DefaultTimeout = 60 * time.Second
	}
	p := &Client{cfg: cfg, pending: map[string]chan v1.Response{}, done: make(chan struct{}), semaphore: make(chan struct{}, 1)}
	p.stderr.max = cfg.MaxMessageBytes
	p.cmd = exec.Command(cfg.Command[0], cfg.Command[1:]...)
	p.cmd.Dir = cfg.Directory
	stdout, err := p.cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stdin, err := p.cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	p.stdin = stdin
	p.cmd.Stderr = &p.stderr
	if err = p.cmd.Start(); err != nil {
		return nil, fmt.Errorf("start plugin: %w", err)
	}
	go p.readLoop(stdout)
	go p.waitLoop()
	var init v1.InitializeResponse
	if err = p.call(ctx, "plugin.initialize", v1.InitializeRequest{ProtocolVersion: v1.ProtocolVersion, ClientName: "GoHermit", MaxMessageSize: cfg.MaxMessageBytes}, &init); err != nil {
		p.kill()
		return nil, fmt.Errorf("initialize plugin: %w", err)
	}
	if init.ProtocolVersion != v1.ProtocolVersion {
		p.kill()
		return nil, fmt.Errorf("plugin protocol version %q is unsupported", init.ProtocolVersion)
	}
	cap := init.Capabilities.MaxConcurrency
	if cap < 1 {
		cap = 1
	}
	if cap > 64 {
		cap = 64
	}
	p.semaphore = make(chan struct{}, cap)
	p.initialized = true
	return p, nil
}
func (p *Client) readLoop(r io.Reader) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64<<10), p.cfg.MaxMessageBytes)
	for scanner.Scan() {
		line := scanner.Bytes()
		var response v1.Response
		if err := json.Unmarshal(line, &response); err != nil || response.JSONRPC != "2.0" || response.ID == "" {
			p.fail(fmt.Errorf("plugin emitted invalid JSON-RPC response"))
			p.kill()
			return
		}
		p.mu.Lock()
		ch := p.pending[response.ID]
		delete(p.pending, response.ID)
		p.mu.Unlock()
		if ch != nil {
			ch <- response
			close(ch)
		}
	}
	err := scanner.Err()
	if err == nil {
		err = io.EOF
	}
	p.fail(fmt.Errorf("plugin stdout closed: %w", err))
}
func (p *Client) waitLoop() {
	err := p.cmd.Wait()
	p.fail(fmt.Errorf("plugin exited: %w", err))
	select {
	case <-p.done:
	default:
		close(p.done)
	}
}
func (p *Client) fail(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.terminal == nil {
		p.terminal = err
	}
	for id, ch := range p.pending {
		delete(p.pending, id)
		close(ch)
	}
}
func (p *Client) call(ctx context.Context, method string, params, out any) error {
	return p.callWithCancel(ctx, method, params, out, "")
}
func (p *Client) callWithCancel(ctx context.Context, method string, params, out any, cancelID string) error {
	id := strconv.FormatUint(p.next.Add(1), 10)
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	req := v1.Request{JSONRPC: "2.0", ID: id, Method: method, Params: raw}
	line, err := json.Marshal(req)
	if err != nil {
		return err
	}
	if len(line) > p.cfg.MaxMessageBytes {
		return errors.New("plugin request exceeds maximum message size")
	}
	ch := make(chan v1.Response, 1)
	p.mu.Lock()
	if p.terminal != nil {
		err = p.terminal
		p.mu.Unlock()
		return err
	}
	p.pending[id] = ch
	p.mu.Unlock()
	p.sendMu.Lock()
	_, err = p.stdin.Write(append(line, '\n'))
	p.sendMu.Unlock()
	if err != nil {
		p.remove(id)
		return err
	}
	select {
	case <-ctx.Done():
		p.remove(id)
		if cancelID == "" {
			cancelID = id
		}
		_ = p.notify("tools.cancel", v1.CancelRequest{RequestID: cancelID})
		return ctx.Err()
	case response, ok := <-ch:
		if !ok {
			p.mu.Lock()
			err = p.terminal
			p.mu.Unlock()
			if err == nil {
				err = errors.New("plugin stopped")
			}
			return err
		}
		if response.Error != nil {
			return fmt.Errorf("plugin RPC %d: %s", response.Error.Code, response.Error.Message)
		}
		if out != nil && len(response.Result) > 0 {
			if err = json.Unmarshal(response.Result, out); err != nil {
				return fmt.Errorf("decode plugin response: %w", err)
			}
		}
		return nil
	}
}
func (p *Client) notify(method string, params any) error {
	raw, _ := json.Marshal(params)
	line, err := json.Marshal(v1.Request{JSONRPC: "2.0", Method: method, Params: raw})
	if err != nil {
		return err
	}
	p.sendMu.Lock()
	defer p.sendMu.Unlock()
	_, err = p.stdin.Write(append(line, '\n'))
	return err
}
func (p *Client) remove(id string) { p.mu.Lock(); delete(p.pending, id); p.mu.Unlock() }
func (p *Client) Tools(ctx context.Context) ([]v1.ToolDefinition, error) {
	ctx, cancel := context.WithTimeout(ctx, p.cfg.DefaultTimeout)
	defer cancel()
	var response v1.ToolsListResponse
	if err := p.call(ctx, "tools.list", struct{}{}, &response); err != nil {
		return nil, err
	}
	return response.Tools, nil
}
func (p *Client) Execute(ctx context.Context, name string, args json.RawMessage) (v1.ExecuteResponse, error) {
	select {
	case p.semaphore <- struct{}{}:
		defer func() { <-p.semaphore }()
	case <-ctx.Done():
		return v1.ExecuteResponse{}, ctx.Err()
	}
	deadline := p.cfg.DefaultTimeout
	if d, ok := ctx.Deadline(); ok {
		deadline = time.Until(d)
	}
	callCtx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()
	var response v1.ExecuteResponse
	id := "tool-" + strconv.FormatUint(p.next.Add(1), 10)
	err := p.callWithCancel(callCtx, "tools.execute", v1.ExecuteRequest{RequestID: id, Name: name, Arguments: args, TimeoutMS: deadline.Milliseconds()}, &response, id)
	return response, err
}
func (p *Client) Health(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, p.cfg.DefaultTimeout)
	defer cancel()
	var health v1.HealthResponse
	if err := p.call(ctx, "plugin.health", struct{}{}, &health); err != nil {
		return err
	}
	if health.Status != "ok" {
		return fmt.Errorf("plugin health is %q", health.Status)
	}
	return nil
}
func (p *Client) Shutdown(ctx context.Context) error {
	if !p.initialized {
		return nil
	}
	var ignored json.RawMessage
	_ = p.call(ctx, "plugin.shutdown", struct{}{}, &ignored)
	_ = p.stdin.Close()
	select {
	case <-p.done:
		return nil
	case <-ctx.Done():
		p.kill()
		return ctx.Err()
	}
}
func (p *Client) kill() {
	if p.cmd != nil && p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
}
func (p *Client) Stderr() string { return p.stderr.String() }

type remoteTool struct {
	client *Client
	def    core.Definition
	remote string
}

func (t remoteTool) Definition() core.Definition { return t.def }
func (t remoteTool) Execute(ctx context.Context, call core.Call) (core.Result, error) {
	response, err := t.client.Execute(ctx, t.remote, call.Arguments)
	if err != nil {
		return core.Result{}, err
	}
	result := core.Result{Output: response.Output}
	if response.IsError {
		code := response.ErrorCode
		if code == "" {
			code = "plugin_tool_error"
		}
		result.Error = &core.Error{Code: code, Message: response.Output}
	}
	return result, nil
}

// RegisterTools discovers and registers namespaced tools from a plugin.
func RegisterTools(ctx context.Context, registry *core.Registry, namespace string, client *Client) error {
	definitions, err := client.Tools(ctx)
	if err != nil {
		return err
	}
	for _, remote := range definitions {
		if remote.Name == "" || !json.Valid(remote.InputSchema) {
			return errors.New("plugin returned an invalid tool definition")
		}
		permission := core.PermissionExecute
		switch remote.Permission {
		case "read":
			permission = core.PermissionRead
		case "write":
			permission = core.PermissionWrite
		}
		timeout := client.cfg.DefaultTimeout
		if remote.TimeoutMS > 0 {
			timeout = time.Duration(remote.TimeoutMS) * time.Millisecond
		}
		name := "plugin." + namespace + "." + remote.Name
		adapter := remoteTool{client: client, remote: remote.Name, def: core.Definition{Name: name, Description: remote.Description, InputSchema: remote.InputSchema, Permission: permission, MutatesWorkspace: remote.MutatesWorkspace, DefaultTimeout: timeout, MaxOutputBytes: client.cfg.MaxMessageBytes}}
		if err := registry.Register(adapter); err != nil {
			return err
		}
	}
	return nil
}
