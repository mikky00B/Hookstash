import React, { useCallback, useEffect, useMemo, useState } from "react";
import { createRoot } from "react-dom/client";
import "./styles.css";

const SAMPLE_CURL = `curl -X POST <your-capture-url> \\
  -H "Content-Type: application/json" \\
  -d '{"event":"charge.success","amount":5000}'`;

function App() {
  const [requests, setRequests] = useState([]);
  const [endpoints, setEndpoints] = useState([]);
  const [activeEndpoint, setActiveEndpoint] = useState("all");
  const [selectedID, setSelectedID] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [liveStatus, setLiveStatus] = useState("Offline");
  const [tunnelStatus, setTunnelStatus] = useState(null);
  const [tunnelRefresh, setTunnelRefresh] = useState(0);

  const loadEndpoints = useCallback(async () => {
    try {
      const response = await fetch("/api/endpoints");
      if (!response.ok) {
        throw new Error(`Request failed with ${response.status}`);
      }
      const data = await response.json();
      setEndpoints(Array.isArray(data.endpoints) ? data.endpoints : []);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not load endpoints");
    }
  }, []);

  const loadRequests = useCallback(
    async ({ showLoading = false } = {}) => {
      if (showLoading) {
        setLoading(true);
      }
      try {
        const url =
          activeEndpoint === "all"
            ? "/api/requests"
            : `/api/requests?endpoint=${encodeURIComponent(activeEndpoint)}`;
        const response = await fetch(url);
        if (!response.ok) {
          const data = await response.json().catch(() => ({}));
          throw new Error(data.error || `Request failed with ${response.status}`);
        }
        const data = await response.json();
        const nextRequests = Array.isArray(data.requests) ? data.requests : [];
        setRequests(nextRequests);
        setSelectedID((current) =>
          nextRequests.some((request) => request.id === current) ? current : nextRequests[0]?.id ?? null
        );
        setError("");
      } catch (err) {
        setError(err instanceof Error ? err.message : "Could not load requests");
      } finally {
        setLoading(false);
      }
    },
    [activeEndpoint]
  );

  const loadTunnel = useCallback(async () => {
    try {
      const response = await fetch("/api/tunnel");
      if (response.ok) {
        setTunnelStatus(await response.json());
      }
    } catch {
      // tunnel status is optional UI; ignore fetch failures
    }
  }, []);

  useEffect(() => {
    loadEndpoints();
  }, [loadEndpoints]);

  useEffect(() => {
    loadTunnel();
  }, [loadTunnel, tunnelRefresh]);

  useEffect(() => {
    loadRequests({ showLoading: true });
  }, [loadRequests]);

  useEffect(() => {
    if (!("EventSource" in window)) {
      setLiveStatus("Offline");
      return undefined;
    }

    const events = new EventSource("/api/events");
    setLiveStatus("Reconnecting");

    events.onopen = () => {
      setLiveStatus("Live");
    };

    events.onerror = () => {
      setLiveStatus(events.readyState === EventSource.CLOSED ? "Offline" : "Reconnecting");
    };

    events.addEventListener("request.created", () => {
      loadRequests();
    });

    for (const eventType of ["tunnel.starting", "tunnel.started", "tunnel.error", "tunnel.stopped"]) {
      events.addEventListener(eventType, () => {
        setTunnelRefresh((value) => value + 1);
      });
    }

    return () => {
      events.close();
    };
  }, [loadRequests]);

  const selected = useMemo(
    () => requests.find((request) => request.id === selectedID) ?? requests[0] ?? null,
    [requests, selectedID]
  );

  const endpointSlugs = useMemo(() => {
    const slugs = {};
    for (const endpoint of endpoints) {
      slugs[endpoint.id] = endpoint.slug;
    }
    return slugs;
  }, [endpoints]);

  const captureURL = `${window.location.origin}/hooks/${
    activeEndpoint === "all" ? "default" : activeEndpoint
  }`;

  return (
    <main className="app-shell">
      <header className="topbar">
        <div>
          <p className="eyebrow">Local webhook inspector</p>
          <h1>Hookstash</h1>
        </div>
        <div className="endpoint-card">
          <span>Capture URL {activeEndpoint !== "all" ? `· /hooks/${activeEndpoint}` : ""}</span>
          <code>{captureURL}</code>
        </div>
        <LiveIndicator status={liveStatus} />
      </header>

      {error && <div className="alert">Could not load captured requests: {error}</div>}

      <EndpointBar
        endpoints={endpoints}
        activeEndpoint={activeEndpoint}
        onSelect={setActiveEndpoint}
        onChanged={loadEndpoints}
      />

      <TunnelCard status={tunnelStatus} onChanged={loadTunnel} />

      {loading ? (
        <div className="panel loading-panel">Loading captured requests...</div>
      ) : requests.length === 0 ? (
        <EmptyState captureURL={captureURL} />
      ) : (
        <section className="dashboard-grid">
          <RequestList
            requests={requests}
            selectedID={selected?.id}
            onSelect={setSelectedID}
            endpointSlugs={endpointSlugs}
          />
          <RequestDetail request={selected} endpointSlug={endpointSlugs[selected?.endpoint_id] || "unknown"} />
        </section>
      )}
    </main>
  );
}

function EndpointBar({ endpoints, activeEndpoint, onSelect, onChanged }) {
  const [creating, setCreating] = useState(false);
  const [slug, setSlug] = useState("");
  const [withToken, setWithToken] = useState(false);
  const [createdToken, setCreatedToken] = useState("");
  const [createError, setCreateError] = useState("");
  const [submitting, setSubmitting] = useState(false);

  async function createEndpoint(event) {
    event.preventDefault();
    setCreateError("");
    setCreatedToken("");
    setSubmitting(true);
    try {
      const response = await fetch("/api/endpoints", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ slug, with_token: withToken })
      });
      const data = await response.json();
      if (!response.ok) {
        throw new Error(data.error || `Create failed with ${response.status}`);
      }
      setCreatedToken(data.token || "");
      setSlug("");
      setWithToken(false);
      await onChanged();
      onSelect(data.endpoint?.slug || slug);
    } catch (err) {
      setCreateError(err instanceof Error ? err.message : "Could not create endpoint");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <section className="panel endpoint-bar" aria-label="Endpoints">
      <div className="endpoint-chips">
        <button
          type="button"
          className={`chip ${activeEndpoint === "all" ? "chip-active" : ""}`}
          onClick={() => onSelect("all")}
        >
          All endpoints
        </button>
        {endpoints.map((endpoint) => (
          <button
            type="button"
            key={endpoint.id}
            className={`chip ${activeEndpoint === endpoint.slug ? "chip-active" : ""}`}
            onClick={() => onSelect(endpoint.slug)}
            title={endpoint.token_hash ? "Token required" : endpoint.provider || undefined}
          >
            /{endpoint.slug}
          </button>
        ))}
        <button type="button" className="chip chip-new" onClick={() => setCreating((value) => !value)}>
          {creating ? "Cancel" : "+ New endpoint"}
        </button>
      </div>

      {creating && (
        <form className="endpoint-form" onSubmit={createEndpoint}>
          <label>
            <span>Name</span>
            <input
              type="text"
              value={slug}
              onChange={(event) => setSlug(event.target.value)}
              placeholder="payments"
              pattern="[A-Za-z0-9_-]+"
              required
            />
          </label>
          <label className="checkbox-label">
            <input
              type="checkbox"
              checked={withToken}
              onChange={(event) => setWithToken(event.target.checked)}
            />
            <span>Require capture token</span>
          </label>
          <button type="submit" disabled={submitting}>
            {submitting ? "Creating..." : "Create endpoint"}
          </button>
        </form>
      )}

      {createError && <div className="alert">{createError}</div>}

      {createdToken && (
        <div className="token-alert">
          <strong>Capture token (shown once — copy it now)</strong>
          <code>{createdToken}</code>
          <button
            type="button"
            onClick={() => navigator.clipboard?.writeText(createdToken).catch(() => {})}
          >
            Copy
          </button>
        </div>
      )}
    </section>
  );
}

function TunnelCard({ status, onChanged }) {
  const [busy, setBusy] = useState(false);
  const state = status?.state || "disabled";

  async function tunnelAction(action) {
    setBusy(true);
    try {
      await fetch(`/api/tunnel/${action}`, { method: "POST" });
      await onChanged();
    } finally {
      setBusy(false);
    }
  }

  const canStart = ["disabled", "stopped", "error"].includes(state);
  const canStop = ["starting", "running"].includes(state);

  return (
    <section className="panel tunnel-card" aria-label="Public URL">
      <div className="tunnel-row">
        <span className={`tunnel-state tunnel-state-${state}`}>{state}</span>
        <strong>Public URL</strong>
        {status?.url && <code className="tunnel-url">{status.url}</code>}
        {status?.url && (
          <button type="button" onClick={() => navigator.clipboard?.writeText(status.url).catch(() => {})}>
            Copy
          </button>
        )}
        {canStart && (
          <button type="button" disabled={busy} onClick={() => tunnelAction("start")}>
            {busy ? "Working..." : "Start cloudflared tunnel"}
          </button>
        )}
        {canStop && (
          <button type="button" disabled={busy} onClick={() => tunnelAction("stop")}>
            {busy ? "Working..." : "Stop"}
          </button>
        )}
      </div>
      {state === "starting" && <p className="tunnel-note">Waiting for cloudflared to assign a URL...</p>}
      {state === "error" && (
        <div className="tunnel-problem">
          <p>{status?.error || "The tunnel failed."}</p>
          {status?.hint && <p className="tunnel-hint">{status.hint}</p>}
        </div>
      )}
      {state === "external" && (
        <p className="tunnel-note">Displayed from --tunnel-url. You manage this tunnel yourself.</p>
      )}
      {state === "disabled" && (
        <p className="tunnel-note">
          Start a free Cloudflare quick tunnel (no account) to receive real provider webhooks.
          Requires the cloudflared binary on your PATH.
        </p>
      )}
    </section>
  );
}

function LiveIndicator({ status }) {
  return (
    <div className={`live-indicator live-${status.toLowerCase()}`} aria-live="polite">
      <span />
      {status}
    </div>
  );
}

function EmptyState({ captureURL }) {
  const sample = SAMPLE_CURL.replace("<your-capture-url>", captureURL);
  return (
    <section className="empty-state">
      <div>
        <p className="eyebrow">No webhooks yet</p>
        <h2>Send a request to Hookstash to see it here.</h2>
        <p>
          Captured webhooks will appear with their headers, body, provider hint, and forwarding
          status.
        </p>
      </div>
      <pre><code>{sample}</code></pre>
    </section>
  );
}

function RequestList({ requests, selectedID, onSelect, endpointSlugs }) {
  return (
    <aside className="panel request-list" aria-label="Captured requests">
      <div className="panel-heading">
        <h2>Requests</h2>
        <span>{requests.length}</span>
      </div>
      <div className="request-items">
        {requests.map((request) => (
          <button
            className={`request-row ${request.id === selectedID ? "selected" : ""}`}
            key={request.id}
            onClick={() => onSelect(request.id)}
            type="button"
          >
            <span className={`method method-${request.method.toLowerCase()}`}>
              {request.method}
            </span>
            <span className="request-main">
              <strong>{pathWithQuery(request)}</strong>
              <small>{formatDate(request.received_at)}</small>
            </span>
            <span className="request-meta">
              <Badge value={endpointSlugs?.[request.endpoint_id] || request.provider_hint || "unknown"} />
              <ForwardStatus request={request} />
            </span>
          </button>
        ))}
      </div>
    </aside>
  );
}

function RequestDetail({ request, endpointSlug }) {
  const [targetURL, setTargetURL] = useState("");
  const [replayResult, setReplayResult] = useState(null);
  const [replayError, setReplayError] = useState("");
  const [replaying, setReplaying] = useState(false);

  useEffect(() => {
    setTargetURL(request?.target_url || "");
    setReplayResult(null);
    setReplayError("");
    setReplaying(false);
  }, [request?.id, request?.target_url]);

  if (!request) {
    return null;
  }

  const headers = parseHeaders(request.headers_json);

  async function replayRequest(event) {
    event.preventDefault();
    setReplayError("");
    setReplayResult(null);
    setReplaying(true);

    try {
      const response = await fetch(`/api/requests/${request.id}/replay`, {
        method: "POST",
        headers: {
          "Content-Type": "application/json"
        },
        body: JSON.stringify({ target_url: targetURL.trim() })
      });
      const data = await response.json();
      if (!response.ok) {
        throw new Error(data.error || `Replay failed with ${response.status}`);
      }
      setReplayResult(data);
    } catch (err) {
      setReplayError(err instanceof Error ? err.message : "Replay failed");
    } finally {
      setReplaying(false);
    }
  }

  return (
    <section className="panel detail-panel">
      <div className="detail-header">
        <div>
          <p className="eyebrow">{request.id}</p>
          <h2>{request.method} {pathWithQuery(request)}</h2>
        </div>
        <ForwardStatus request={request} />
      </div>

      <dl className="summary-grid">
        <Info label="Endpoint" value={endpointSlug || "unknown"} />
        <Info label="Content type" value={request.content_type || "not provided"} />
        <Info label="Provider" value={request.provider_hint || "unknown"} />
        <Info label="Forward status" value={request.forward_status || "unknown"} />
        <Info label="Status code" value={request.forward_status_code ?? "none"} />
        <Info label="Target URL" value={request.target_url || "not configured"} />
        <Info label="Duration" value={formatDuration(request.forward_duration_ms)} />
      </dl>

      {request.forward_error && (
        <div className="error-box">
          <strong>Forward error</strong>
          <span>{request.forward_error}</span>
        </div>
      )}

      <form className="replay-box" onSubmit={replayRequest}>
        <div>
          <h3>Replay</h3>
          <p>Replay the captured request body and headers to a local target URL.</p>
        </div>
        <label>
          <span>Target URL</span>
          <input
            type="url"
            value={targetURL}
            onChange={(event) => setTargetURL(event.target.value)}
            placeholder="http://127.0.0.1:8000/webhooks"
            required
          />
        </label>
        <button type="submit" disabled={replaying}>
          {replaying ? "Replaying..." : "Replay"}
        </button>

        {replayError && (
          <div className="replay-result replay-result-error">
            <strong>Replay error</strong>
            <span>{replayError}</span>
          </div>
        )}

        {replayResult && (
          <div className={replayResult.error ? "replay-result replay-result-error" : "replay-result replay-result-success"}>
            <strong>{replayResult.error ? "Replay failed" : "Replay sent"}</strong>
            <span>
              Status: {replayResult.status_code ?? "none"} · Duration: {formatDuration(replayResult.duration_ms)}
            </span>
            {replayResult.error && <span>{replayResult.error}</span>}
            {replayResult.response_body && (
              <pre><code>{formatBody(replayResult.response_body)}</code></pre>
            )}
          </div>
        )}
      </form>

      <div className="detail-section">
        <h3>Body</h3>
        <pre className="code-block"><code>{formatBody(request.body_text)}</code></pre>
      </div>

      <div className="detail-section">
        <h3>Headers</h3>
        <pre className="code-block"><code>{JSON.stringify(headers, null, 2)}</code></pre>
      </div>
    </section>
  );
}

function Info({ label, value }) {
  return (
    <div>
      <dt>{label}</dt>
      <dd>{value}</dd>
    </div>
  );
}

function Badge({ value }) {
  return <span className="badge">{value}</span>;
}

function ForwardStatus({ request }) {
  const status = request.forward_status || "unknown";
  const statusCode = request.forward_status_code ? ` ${request.forward_status_code}` : "";
  return <span className={`status status-${status}`}>{status}{statusCode}</span>;
}

function parseHeaders(headersJSON) {
  if (!headersJSON) {
    return {};
  }
  try {
    return JSON.parse(headersJSON);
  } catch {
    return { raw: headersJSON };
  }
}

function formatBody(bodyText) {
  if (!bodyText) {
    return "";
  }
  try {
    return JSON.stringify(JSON.parse(bodyText), null, 2);
  } catch {
    return bodyText;
  }
}

function pathWithQuery(request) {
  return request.query_string ? `${request.path}?${request.query_string}` : request.path;
}

function formatDate(value) {
  if (!value) {
    return "unknown time";
  }
  return new Intl.DateTimeFormat(undefined, {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit"
  }).format(new Date(value));
}

function formatDuration(value) {
  if (value === null || value === undefined) {
    return "none";
  }
  return `${value} ms`;
}

createRoot(document.getElementById("root")).render(<App />);
