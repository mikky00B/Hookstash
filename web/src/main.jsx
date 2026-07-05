import React, { useEffect, useMemo, useState } from "react";
import { createRoot } from "react-dom/client";
import "./styles.css";

const webhookEndpoint = `${window.location.origin}/hooks/default`;
const SAMPLE_CURL = `curl -X POST ${webhookEndpoint} \\
  -H "Content-Type: application/json" \\
  -d '{"event":"charge.success","amount":5000}'`;

function App() {
  const [requests, setRequests] = useState([]);
  const [selectedID, setSelectedID] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  useEffect(() => {
    let cancelled = false;

    async function loadRequests() {
      try {
        const response = await fetch("/api/requests");
        if (!response.ok) {
          throw new Error(`Request failed with ${response.status}`);
        }
        const data = await response.json();
        const nextRequests = Array.isArray(data.requests) ? data.requests : [];
        if (!cancelled) {
          setRequests(nextRequests);
          setSelectedID((current) => current ?? nextRequests[0]?.id ?? null);
          setError("");
        }
      } catch (err) {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : "Could not load requests");
        }
      } finally {
        if (!cancelled) {
          setLoading(false);
        }
      }
    }

    loadRequests();
    return () => {
      cancelled = true;
    };
  }, []);

  const selected = useMemo(
    () => requests.find((request) => request.id === selectedID) ?? requests[0] ?? null,
    [requests, selectedID]
  );

  return (
    <main className="app-shell">
      <header className="topbar">
        <div>
          <p className="eyebrow">Local webhook inspector</p>
          <h1>Hookstash</h1>
        </div>
        <div className="endpoint-card">
          <span>Webhook endpoint</span>
          <code>{webhookEndpoint}</code>
        </div>
      </header>

      {error && <div className="alert">Could not load captured requests: {error}</div>}

      {loading ? (
        <div className="panel loading-panel">Loading captured requests...</div>
      ) : requests.length === 0 ? (
        <EmptyState />
      ) : (
        <section className="dashboard-grid">
          <RequestList requests={requests} selectedID={selected?.id} onSelect={setSelectedID} />
          <RequestDetail request={selected} />
        </section>
      )}
    </main>
  );
}

function EmptyState() {
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
      <pre><code>{SAMPLE_CURL}</code></pre>
    </section>
  );
}

function RequestList({ requests, selectedID, onSelect }) {
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
              <Badge value={request.provider_hint || "unknown"} />
              <ForwardStatus request={request} />
            </span>
          </button>
        ))}
      </div>
    </aside>
  );
}

function RequestDetail({ request }) {
  if (!request) {
    return null;
  }

  const headers = parseHeaders(request.headers_json);

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
