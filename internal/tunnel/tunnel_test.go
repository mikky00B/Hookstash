package tunnel

import (
	"strings"
	"testing"
)

func TestExtractTunnelURL(t *testing.T) {
	cases := []struct {
		name    string
		line    string
		wantURL string
	}{
		{
			name:    "plain url line",
			line:    "  https://example-random-words.trycloudflare.com  ",
			wantURL: "https://example-random-words.trycloudflare.com",
		},
		{
			name:    "log-prefixed url line",
			line:    "2026-09-13T12:00:00Z INF |  https://foo-bar-baz.trycloudflare.com  |",
			wantURL: "https://foo-bar-baz.trycloudflare.com",
		},
		{
			name:    "banner without url",
			line:    "+-----------------------------------------------+",
			wantURL: "",
		},
		{
			name:    "unrelated log line",
			line:    "2026-09-13T12:00:00Z INF Starting metrics server",
			wantURL: "",
		},
		{
			name:    "other domain ignored",
			line:    "visit https://example.com now",
			wantURL: "",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := ExtractTunnelURL(testCase.line); got != testCase.wantURL {
				t.Fatalf("url = %q, want %q", got, testCase.wantURL)
			}
		})
	}
}

func TestNewManagerStates(t *testing.T) {
	var events []string
	manager := NewManager(Config{TargetURL: "http://127.0.0.1:4040", OnEvent: func(event string, status Status) {
		events = append(events, event)
	}})
	if got := manager.Status().State; got != StateDisabled {
		t.Fatalf("default state = %q, want disabled", got)
	}

	external := NewManager(Config{TargetURL: "http://127.0.0.1:4040", ExternalURL: "https://hooks.example.com"})
	status := external.Status()
	if status.State != StateExternal || status.URL != "https://hooks.example.com" {
		t.Fatalf("external status = %+v", status)
	}
	// Starting an external tunnel must be a no-op that reports the same status.
	if got := external.Start(); got.State != StateExternal {
		t.Fatalf("external start state = %q, want external", got.State)
	}
	if len(events) != 0 {
		t.Fatalf("unexpected events: %v", events)
	}
}

func TestLineWriterSplitsLines(t *testing.T) {
	lines := make(chan string, 8)
	writer := &lineWriter{lines: lines}

	writer.Write([]byte("first line\nsecond "))
	writer.Write([]byte("line\r\nthird-no-newline"))

	if got := <-lines; got != "first line" {
		t.Fatalf("first line = %q", got)
	}
	if got := <-lines; got != "second line" {
		t.Fatalf("second line = %q", got)
	}
	select {
	case got := <-lines:
		t.Fatalf("unexpected extra line %q (partial lines must wait for the newline)", got)
	default:
	}
}

func TestStartFailsWhenBinaryMissing(t *testing.T) {
	manager := NewManager(Config{
		TargetURL: "http://127.0.0.1:4040",
		Binary:    "hookstash-nonexistent-cloudflared-" + strings.Repeat("x", 8),
	})
	status := manager.Start()
	if status.State != StateError {
		t.Fatalf("state = %q, want error", status.State)
	}
	if status.Error == "" || status.Hint == "" {
		t.Fatalf("expected error and hint, got %+v", status)
	}
	// A stopped manager must be restartable.
	if got := manager.Stop(); got.State != StateError {
		t.Fatalf("stop state = %q, want unchanged error", got.State)
	}
}
