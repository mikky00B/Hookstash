package tunnel

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

// States reported in Status.State.
const (
	StateDisabled = "disabled" // no tunnel requested
	StateExternal = "external" // user manages the tunnel, we only display the URL
	StateStarting = "starting" // cloudflared is booting, no URL yet
	StateRunning  = "running"  // quick tunnel URL captured
	StateError    = "error"    // cloudflared missing, failed, or timed out
	StateStopped  = "stopped"  // was running and was stopped
)

const (
	// urlTimeout bounds how long we wait for cloudflared to print the
	// quick-tunnel URL before giving up.
	urlTimeout = 45 * time.Second
	stopWait   = 5 * time.Second
)

var quickTunnelURL = regexp.MustCompile(`https://[a-zA-Z0-9-]+\.trycloudflare\.com`)

// Status is the observable state of the tunnel.
type Status struct {
	State string `json:"state"`
	URL   string `json:"url,omitempty"`
	Error string `json:"error,omitempty"`
	Hint  string `json:"hint,omitempty"`
}

// EventFunc is called on state transitions so the server can publish SSE
// events. It must not call back into the Manager synchronously.
type EventFunc func(event string, status Status)

type Config struct {
	// TargetURL is the local address cloudflared forwards to, e.g.
	// http://127.0.0.1:4040.
	TargetURL string
	// ExternalURL, when set, makes the manager display-only: the user runs
	// their own (named) tunnel and Hookstash never spawns anything.
	ExternalURL string
	// Binary is the cloudflared executable; empty means "cloudflared" on PATH.
	Binary string
	// OnEvent receives state transition notifications; may be nil.
	OnEvent EventFunc
}

type Manager struct {
	cfg Config

	mu          sync.Mutex
	status      Status
	cancel      context.CancelFunc
	done        chan struct{}
	lastErrLine string
}

func NewManager(cfg Config) *Manager {
	if cfg.Binary == "" {
		cfg.Binary = "cloudflared"
	}
	m := &Manager{cfg: cfg}
	switch {
	case cfg.ExternalURL != "":
		m.status = Status{State: StateExternal, URL: cfg.ExternalURL}
	default:
		m.status = Status{State: StateDisabled}
	}
	return m
}

// Status returns the current tunnel state.
func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status
}

// Start launches cloudflared as a quick tunnel (or returns the existing
// status if the tunnel is external, already starting, or already running).
func (m *Manager) Start() Status {
	m.mu.Lock()
	if m.cfg.ExternalURL != "" {
		status := m.status
		m.mu.Unlock()
		return status
	}
	if m.status.State == StateStarting || m.status.State == StateRunning {
		status := m.status
		m.mu.Unlock()
		return status
	}
	m.stopLocked()

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, m.cfg.Binary, "tunnel", "--url", m.cfg.TargetURL, "--no-autoupdate")
	lines := make(chan string, 64)
	cmd.Stdout = &lineWriter{lines: lines}
	cmd.Stderr = &lineWriter{lines: lines}

	if err := cmd.Start(); err != nil {
		cancel()
		m.lastErrLine = ""
		m.status = Status{
			State: StateError,
			Error: fmt.Sprintf("could not start %s: %v", m.cfg.Binary, err),
			Hint:  "Install cloudflared (winget install Cloudflare.cloudflared) and make sure it is on your PATH, or run Hookstash with --tunnel-url to display a tunnel you manage yourself.",
		}
		status := m.status
		m.mu.Unlock()
		m.emit("tunnel.error", status)
		return status
	}

	m.cancel = cancel
	m.done = make(chan struct{})
	m.lastErrLine = ""
	m.status = Status{State: StateStarting}
	starting := m.status
	done := m.done
	m.mu.Unlock()
	m.emit("tunnel.starting", starting)

	go func() {
		defer close(done)
		_ = cmd.Wait()
	}()

	// Close the line channel shortly after exit so `watch` observes EOF and
	// reports the failure immediately instead of waiting for the deadline.
	go func() {
		<-done
		time.Sleep(500 * time.Millisecond)
		close(lines)
	}()

	go m.watch(ctx, lines, done)
	return starting
}

// watch consumes cloudflared output until the URL appears, the process dies,
// the deadline passes, or the tunnel is stopped.
func (m *Manager) watch(ctx context.Context, lines <-chan string, done <-chan struct{}) {
	deadline := time.After(urlTimeout)
	var announced bool

	for {
		select {
		case line, ok := <-lines:
			if !ok {
				// Process exited. If it never produced a URL, that is an error.
				if !announced {
					status := m.fail("cloudflared exited before providing a tunnel URL", true)
					m.emit("tunnel.error", status)
					return
				}
				status := m.markStopped("cloudflared process exited")
				m.emit("tunnel.error", status)
				return
			}
			if url := ExtractTunnelURL(line); url != "" {
				if status := m.markRunning(url); status != nil {
					announced = true
					m.emit("tunnel.started", *status)
				}
			} else if strings.Contains(line, "ERR") || strings.Contains(line, "failed") {
				m.mu.Lock()
				m.lastErrLine = strings.TrimSpace(line)
				m.mu.Unlock()
			}
		case <-deadline:
			if !announced {
				status := m.fail("timed out waiting for a tunnel URL", true)
				m.emit("tunnel.error", status)
			}
		case <-ctx.Done():
			// Stopped by Stop(); it owns the final status.
			return
		case <-done:
			// Wait() finished but the lines channel may still be open; the
			// ok==false branch above handles the terminal transition.
			if announced {
				return
			}
		}
	}
}

// Stop terminates the cloudflared process, if any.
func (m *Manager) Stop() Status {
	m.mu.Lock()
	if m.cfg.ExternalURL != "" || m.status.State == StateDisabled || m.cancel == nil {
		status := m.status
		m.mu.Unlock()
		return status
	}
	cancel := m.cancel
	done := m.done
	m.mu.Unlock()

	cancel()
	if done != nil {
		select {
		case <-done:
		case <-time.After(stopWait):
		}
	}

	m.mu.Lock()
	m.cancel = nil
	m.done = nil
	m.status = Status{State: StateStopped}
	status := m.status
	m.mu.Unlock()
	m.emit("tunnel.stopped", status)
	return status
}

func (m *Manager) markRunning(url string) *Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.status.State == StateStarting {
		m.status = Status{State: StateRunning, URL: url}
		status := m.status
		return &status
	}
	return nil
}

func (m *Manager) markStopped(reason string) Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cancel = nil
	m.done = nil
	m.status = Status{State: StateError, Error: reason}
	return m.status
}

func (m *Manager) fail(reason string, includeLastLine bool) Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
		m.done = nil
	}
	message := reason
	if includeLastLine && m.lastErrLine != "" {
		message = fmt.Sprintf("%s (last output: %s)", message, m.lastErrLine)
	}
	m.status = Status{
		State: StateError,
		Error: message,
		Hint:  "Quick tunnels need the cloudflared binary on your PATH. You can also run Hookstash with --tunnel-url to display a tunnel you manage yourself.",
	}
	return m.status
}

func (m *Manager) stopLocked() {
	if m.cancel != nil {
		m.cancel()
		if m.done != nil {
			select {
			case <-m.done:
			case <-time.After(stopWait):
			}
		}
		m.cancel = nil
		m.done = nil
	}
}

func (m *Manager) emit(event string, status Status) {
	if m.cfg.OnEvent != nil {
		m.cfg.OnEvent(event, status)
	}
}

// ExtractTunnelURL finds the quick-tunnel URL in a cloudflared output line.
func ExtractTunnelURL(line string) string {
	return quickTunnelURL.FindString(line)
}

// lineWriter splits a stream into lines and forwards each to a channel.
type lineWriter struct {
	lines  chan<- string
	buffer []byte
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.buffer = append(w.buffer, p...)
	for {
		index := strings.IndexByte(string(w.buffer), '\n')
		if index < 0 {
			break
		}
		line := strings.TrimRight(string(w.buffer[:index]), "\r")
		w.buffer = w.buffer[index+1:]
		select {
		case w.lines <- line:
		default: // drop output if nobody is reading; never block cloudflared
		}
	}
	return len(p), nil
}
