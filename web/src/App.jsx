import { useState, useEffect, useCallback } from "react";

const API = "/api";

async function api(path, opts) {
  const res = await fetch(`${API}${path}`, opts);
  return res.json();
}

function StatusBadge({ status }) {
  const colors = {
    healthy: "bg-green-100 text-green-800",
    under_replicated: "bg-yellow-100 text-yellow-800",
    unavailable: "bg-red-100 text-red-800",
    online: "bg-green-100 text-green-800",
    offline: "bg-gray-100 text-gray-600",
  };
  return (
    <span className={`inline-block px-2 py-0.5 rounded text-xs font-medium ${colors[status] || "bg-gray-100"}`}>
      {status}
    </span>
  );
}

function formatLastSeen(value) {
  if (!value) return "never seen";
  const ts = new Date(value).getTime();
  if (!Number.isFinite(ts)) return "never seen";
  const seconds = Math.max(0, Math.round((Date.now() - ts) / 1000));
  if (seconds < 5) return "just now";
  if (seconds < 60) return `${seconds}s ago`;
  const minutes = Math.round(seconds / 60);
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.round(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.round(hours / 24);
  return `${days}d ago`;
}

function NodeCard({ node }) {
  const usedPct = node.total_bytes > 0 ? Math.round((node.used_bytes / node.total_bytes) * 100) : 0;
  return (
    <div className="bg-white rounded-lg shadow p-4">
      <div className="flex items-center justify-between mb-2">
        <h3 className="font-semibold text-sm truncate">{node.name}</h3>
        <StatusBadge status={node.status} />
      </div>
      <p className="text-xs text-gray-500 mb-1">{node.platform} · {node.node_id.slice(0, 8)}…</p>
      <p className="text-xs text-gray-500 mb-2">{node.address}</p>
      <p className="text-xs text-gray-500 mb-2">Last seen {formatLastSeen(node.last_seen)}</p>
      <div className="w-full bg-gray-200 rounded-full h-2">
        <div className="bg-blue-600 h-2 rounded-full" style={{ width: `${usedPct}%` }} />
      </div>
      <p className="text-xs text-gray-400 mt-1">{formatBytes(node.used_bytes)} / {formatBytes(node.total_bytes)}</p>
    </div>
  );
}

function FileCard({ file, onDownload, onDelete }) {
  return (
    <div className="bg-white rounded-lg shadow p-4 flex items-center gap-3">
      <div className="text-2xl">{file.is_dir ? "📁" : "📄"}</div>
      <div className="flex-1 min-w-0">
        <p className="font-medium text-sm truncate">{file.name}</p>
        <p className="text-xs text-gray-500">
          {file.is_dir ? "Directory" : formatBytes(file.size_bytes)}
          {file.modified_at && ` · ${new Date(file.modified_at).toLocaleDateString()}`}
        </p>
      </div>
      {!file.is_dir && (
        <div className="flex gap-2">
          <button
            onClick={() => onDownload(file)}
            className="px-3 py-2 bg-blue-50 text-blue-600 rounded-lg text-xs font-medium hover:bg-blue-100 active:bg-blue-200"
          >
            ↓
          </button>
          <button
            onClick={() => onDelete(file)}
            className="px-3 py-2 bg-red-50 text-red-600 rounded-lg text-xs font-medium hover:bg-red-100 active:bg-red-200"
          >
            ✕
          </button>
        </div>
      )}
    </div>
  );
}

function FilesPage() {
  const [path, setPath] = useState("/");
  const [files, setFiles] = useState([]);
  const [search, setSearch] = useState("");
  const [uploading, setUploading] = useState(false);
  const [refreshing, setRefreshing] = useState(false);

  const loadFiles = useCallback(async () => {
    const q = search ? `?q=${encodeURIComponent(search)}` : `?path=${encodeURIComponent(path)}`;
    const res = await api(`/files${q}`);
    if (res.ok) setFiles(res.data.entries || []);
  }, [path, search]);

  useEffect(() => { loadFiles(); }, [loadFiles]);

  const handleRefresh = async () => {
    setRefreshing(true);
    await loadFiles();
    setRefreshing(false);
  };

  const handleUpload = async (e) => {
    const file = e.target.files[0];
    if (!file) return;
    setUploading(true);
    try {
      const formData = new FormData();
      formData.append("path", path === "/" ? `/${file.name}` : `${path}/${file.name}`);
      formData.append("file", file);
      const res = await fetch(`${API}/files/upload`, { method: "POST", body: formData });
      const data = await res.json();
      if (!data.ok) {
        alert(`Upload failed: ${data.error?.message || "Unknown error"}`);
      }
    } catch (err) {
      alert(`Upload error: ${err.message}`);
    } finally {
      setUploading(false);
      loadFiles();
    }
  };

  const handleDownload = (file) => {
    window.open(`${API}/files/download?path=${encodeURIComponent(file.path)}`);
  };

  const handleDelete = async (file) => {
    if (!confirm(`Delete "${file.name}"?`)) return;
    await fetch(`${API}/files?path=${encodeURIComponent(file.path)}`, { method: "DELETE" });
    loadFiles();
  };

  return (
    <div>
      {/* Search and actions */}
      <div className="flex gap-2 mb-4">
        <input
          type="text"
          placeholder="Search files…"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          className="border rounded-lg px-4 py-3 text-sm flex-1"
        />
        <button
          onClick={handleRefresh}
          disabled={refreshing}
          className="px-4 py-3 bg-gray-100 rounded-lg text-sm font-medium hover:bg-gray-200 active:bg-gray-300 disabled:opacity-50"
        >
          {refreshing ? "↻" : "↻"}
        </button>
      </div>

      {/* Upload button */}
      <label className="block w-full mb-4">
        <div className="bg-blue-600 text-white text-center py-3 rounded-lg font-medium cursor-pointer hover:bg-blue-700 active:bg-blue-800">
          {uploading ? "Uploading…" : "⬆ Upload File"}
        </div>
        <input type="file" className="hidden" onChange={handleUpload} disabled={uploading} />
      </label>

      {/* File list */}
      <div className="space-y-2">
        {files.map((f) => (
          <FileCard key={f.file_id || f.path} file={f} onDownload={handleDownload} onDelete={handleDelete} />
        ))}
        {files.length === 0 && (
          <div className="bg-white rounded-lg shadow p-8 text-center text-gray-400 text-sm">
            No files
          </div>
        )}
      </div>
    </div>
  );
}

function NodesPage() {
  const [nodes, setNodes] = useState([]);
  const [invite, setInvite] = useState(null);
  const [creatingInvite, setCreatingInvite] = useState(false);
  const [showSwitch, setShowSwitch] = useState(false);
  const [switchAddr, setSwitchAddr] = useState("");
  const [switchToken, setSwitchToken] = useState("");
  const [switching, setSwitching] = useState(false);
  const [switchError, setSwitchError] = useState(null);

  const handleSwitch = async (e) => {
    e.preventDefault();
    setSwitchError(null);
    setSwitching(true);
    try {
      const res = await api("/join", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ bootstrap: switchAddr, join_token: switchToken }),
      });
      if (res.ok) window.location.reload();
      else setSwitchError(res.error?.message || "Join failed");
    } catch (err) {
      setSwitchError(err.message || "Network error");
    } finally {
      setSwitching(false);
    }
  };

  const loadNodes = useCallback(() => {
    api("/nodes").then((r) => { if (r.ok) setNodes(r.data || []); });
  }, []);

  useEffect(() => {
    loadNodes();
    const id = setInterval(loadNodes, 3000);
    return () => clearInterval(id);
  }, [loadNodes]);

  const totalBytes = nodes.reduce((s, n) => s + (n.total_bytes || 0), 0);
  const usedBytes = nodes.reduce((s, n) => s + (n.used_bytes || 0), 0);
  const onlineCount = nodes.filter((n) => n.status === "online").length;

  const createInvite = async () => {
    setCreatingInvite(true);
    try {
      const res = await api("/invites", { method: "POST" });
      if (res.ok) setInvite(res.data);
    } finally {
      setCreatingInvite(false);
    }
  };

  return (
    <div>
      {/* Stats */}
      <div className="grid grid-cols-3 gap-3 mb-4">
        <div className="bg-white rounded-lg shadow p-3 text-center">
          <p className="text-xl font-bold text-blue-600">{nodes.length}</p>
          <p className="text-xs text-gray-500">Nodes</p>
        </div>
        <div className="bg-white rounded-lg shadow p-3 text-center">
          <p className="text-xl font-bold text-green-600">{onlineCount}</p>
          <p className="text-xs text-gray-500">Online</p>
        </div>
        <div className="bg-white rounded-lg shadow p-3 text-center">
          <p className="text-xl font-bold">{formatBytes(totalBytes)}</p>
          <p className="text-xs text-gray-500">Total</p>
        </div>
      </div>

      {/* Invite */}
      <div className="bg-white rounded-lg shadow p-4 mb-4">
        <div className="flex items-center justify-between gap-3">
          <div>
            <h2 className="font-semibold text-sm">Invite a node</h2>
            <p className="text-xs text-gray-500">One-time token, expires in 15 min</p>
          </div>
          <button
            onClick={createInvite}
            disabled={creatingInvite}
            className="px-4 py-2 bg-blue-600 text-white rounded-lg text-sm font-medium hover:bg-blue-700 disabled:opacity-50"
          >
            {creatingInvite ? "…" : "Create"}
          </button>
        </div>
        {invite && (
          <div className="mt-3 bg-gray-50 rounded-lg p-3">
            <p className="text-xs text-gray-500 mb-1">Join token</p>
            <code className="block text-sm break-all font-mono">{invite.join_token}</code>
            <p className="text-xs text-gray-400 mt-2">Expires {new Date(invite.expires_at).toLocaleString()}</p>
          </div>
        )}
      </div>

      {/* Switch pool */}
      <div className="bg-white rounded-lg shadow p-4 mb-4">
        <button
          onClick={() => setShowSwitch(!showSwitch)}
          className="text-sm text-gray-500 hover:text-gray-700 w-full text-left"
        >
          {showSwitch ? "▲ Hide" : "▼ Join another pool"}…
        </button>
        {showSwitch && (
          <form onSubmit={handleSwitch} className="mt-3 space-y-3">
            <input
              type="text"
              value={switchAddr}
              onChange={(e) => setSwitchAddr(e.target.value)}
              placeholder="http://192.168.1.10:7788"
              required
              className="w-full border rounded-lg px-4 py-3 text-sm"
            />
            <input
              type="text"
              value={switchToken}
              onChange={(e) => setSwitchToken(e.target.value)}
              placeholder="Invite token (optional for auto mode)"
              className="w-full border rounded-lg px-4 py-3 text-sm"
            />
            {switchError && <p className="text-sm text-red-600">{switchError}</p>}
            <button
              type="submit"
              disabled={switching || !switchAddr}
              className="w-full bg-blue-600 text-white py-3 rounded-lg text-sm font-medium hover:bg-blue-700 disabled:opacity-50"
            >
              {switching ? "Joining…" : "Switch pool"}
            </button>
          </form>
        )}
      </div>

      {/* Node list */}
      <div className="space-y-3">
        {nodes.map((n) => <NodeCard key={n.node_id} node={n} />)}
      </div>
    </div>
  );
}

function LogsPage() {
  const [logs, setLogs] = useState([]);
  const [agentLogs, setAgentLogs] = useState([]);
  const [view, setView] = useState("agent");
  const [refreshing, setRefreshing] = useState(false);

  const loadLogs = useCallback(async () => {
    const [eventsRes, agentRes] = await Promise.all([
      api("/logs"),
      api("/agent/logs"),
    ]);
    if (eventsRes.ok) setLogs(eventsRes.data?.entries || []);
    if (agentRes.ok) setAgentLogs(agentRes.data?.lines || []);
  }, []);

  useEffect(() => { loadLogs(); }, [loadLogs]);

  const handleRefresh = async () => {
    setRefreshing(true);
    await loadLogs();
    setRefreshing(false);
  };

  const typeColor = {
    FILE_PUT: "text-green-600",
    FILE_DELETE: "text-red-600",
    NODE_JOIN: "text-blue-600",
    NODE_UPDATE: "text-blue-600",
    CHUNK_REPLICA_ADD: "text-purple-600",
  };

  return (
    <div>
      {/* View toggle */}
      <div className="flex gap-2 mb-4">
        <button
          onClick={() => setView("agent")}
          className={`flex-1 py-2 rounded-lg text-sm font-medium ${
            view === "agent" ? "bg-blue-600 text-white" : "bg-gray-100 text-gray-600"
          }`}
        >
          Agent Logs
        </button>
        <button
          onClick={() => setView("events")}
          className={`flex-1 py-2 rounded-lg text-sm font-medium ${
            view === "events" ? "bg-blue-600 text-white" : "bg-gray-100 text-gray-600"
          }`}
        >
          Events
        </button>
        <button
          onClick={handleRefresh}
          disabled={refreshing}
          className="px-3 py-2 bg-gray-100 rounded-lg text-sm hover:bg-gray-200 disabled:opacity-50"
        >
          ↻
        </button>
      </div>

      {view === "agent" && (
        <div className="bg-gray-900 rounded-lg p-3 max-h-[60vh] overflow-y-auto">
          {agentLogs.length === 0 ? (
            <p className="text-gray-400 text-xs text-center py-4">No agent logs yet</p>
          ) : (
            agentLogs.map((line, i) => (
              <p key={i} className="text-green-400 text-xs font-mono leading-relaxed whitespace-pre-wrap break-all">
                {line}
              </p>
            ))
          )}
        </div>
      )}

      {view === "events" && (
        <div className="space-y-2">
          {logs.map((log, i) => (
            <div key={i} className="bg-white rounded-lg shadow p-3">
              <div className="flex items-center justify-between mb-1">
                <span className={`text-xs font-medium ${typeColor[log.type] || "text-gray-600"}`}>
                  {log.type}
                </span>
                <span className="text-xs text-gray-400">{log.timestamp}</span>
              </div>
              <p className="text-xs text-gray-500 font-mono truncate">Node: {log.node_id?.slice(0, 8)}…</p>
            </div>
          ))}
          {logs.length === 0 && (
            <div className="bg-white rounded-lg shadow p-8 text-center text-gray-400 text-sm">
              No events yet
            </div>
          )}
        </div>
      )}
    </div>
  );
}

function JoinPage({ mode }) {
  const [bootstrap, setBootstrap] = useState("");
  const [token, setToken] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState(null);
  const [discovered, setDiscovered] = useState([]);
  const [selectedAddr, setSelectedAddr] = useState("");
  const [scanning, setScanning] = useState(false);
  const [scanResults, setScanResults] = useState([]);

  useEffect(() => {
    if (mode === "invite") {
      const poll = setInterval(async () => {
        try {
          const r = await api("/nodes/discovered");
          if (r.ok) setDiscovered(r.data || []);
        } catch {}
      }, 3000);
      return () => clearInterval(poll);
    }
  }, [mode]);

  const handleScan = async () => {
    setScanning(true);
    setScanResults([]);
    try {
      const r = await api("/network/scan");
      if (r.ok) setScanResults(r.data?.nodes || []);
    } catch (e) {
      setError(e.message);
    } finally {
      setScanning(false);
    }
  };

  const handleCreateCluster = async () => {
    setLoading(true);
    setError(null);
    try {
      const r = await api("/cluster", { method: "POST" });
      if (r.ok) window.location.reload();
      else setError(r.error?.message || "Failed to create cluster");
    } catch (e) {
      setError(e.message);
    } finally {
      setLoading(false);
    }
  };

  const handleJoin = async (e) => {
    e.preventDefault();
    setLoading(true);
    setError(null);
    try {
      const addr = selectedAddr || bootstrap;
      const r = await api("/join", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ bootstrap: addr, join_token: token }),
      });
      if (r.ok) window.location.reload();
      else setError(r.error?.message || "Join failed");
    } catch (e) {
      setError(e.message);
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="min-h-screen flex items-center justify-center p-4">
      <div className="w-full max-w-sm">
        <div className="text-center mb-8">
          <h1 className="text-2xl font-bold mb-2">PocketCluster</h1>
          <p className="text-gray-500 text-sm">Distributed storage pool</p>
        </div>

        {mode === "invite" && discovered.length > 0 && (
          <div className="bg-white rounded-lg shadow p-4 mb-4">
            <h2 className="font-semibold text-sm mb-3">Discovered nodes</h2>
            <div className="space-y-2">
              {discovered.map((n) => (
                <button
                  key={n.node_id}
                  onClick={() => setSelectedAddr(`http://${n.address}`)}
                  className={`w-full text-left p-3 rounded-lg border ${
                    selectedAddr === `http://${n.address}` ? "border-blue-500 bg-blue-50" : "border-gray-200"
                  }`}
                >
                  <p className="font-medium text-sm">{n.name || n.node_id.slice(0, 8)}</p>
                  <p className="text-xs text-gray-500">{n.platform} · {n.address}</p>
                </button>
              ))}
            </div>
          </div>
        )}

        {/* Network scan */}
        <div className="bg-white rounded-lg shadow p-4 mb-4">
          <div className="flex items-center justify-between mb-3">
            <h2 className="font-semibold text-sm">Scan local network</h2>
            <button
              onClick={handleScan}
              disabled={scanning}
              className="px-3 py-1.5 bg-gray-100 rounded text-sm hover:bg-gray-200 disabled:opacity-50"
            >
              {scanning ? "Scanning…" : "🔍 Scan"}
            </button>
          </div>
          {scanResults.length > 0 && (
            <div className="space-y-2">
              {scanResults.map((n) => (
                <button
                  key={n.node_id}
                  onClick={() => {
                    setSelectedAddr(`http://${n.address}`);
                    setBootstrap(`http://${n.address}`);
                  }}
                  className={`w-full text-left p-3 rounded-lg border ${
                    selectedAddr === `http://${n.address}` ? "border-blue-500 bg-blue-50" : "border-gray-200"
                  }`}
                >
                  <p className="font-medium text-sm">{n.node_id.slice(0, 8)}…</p>
                  <p className="text-xs text-gray-500">{n.address}</p>
                </button>
              ))}
            </div>
          )}
          {scanResults.length === 0 && !scanning && (
            <p className="text-xs text-gray-400">Click scan to find PocketCluster nodes on your network</p>
          )}
        </div>

        <form onSubmit={handleJoin} className="bg-white rounded-lg shadow p-4 mb-4 space-y-3">
          <h2 className="font-semibold text-sm">Join existing pool</h2>
          <input
            type="text"
            value={bootstrap}
            onChange={(e) => setBootstrap(e.target.value)}
            placeholder="http://192.168.1.10:7788"
            className="w-full border rounded-lg px-4 py-3 text-sm"
          />
          <input
            type="text"
            value={token}
            onChange={(e) => setToken(e.target.value)}
            placeholder="Invite token (optional for auto mode)"
            className="w-full border rounded-lg px-4 py-3 text-sm"
          />
          {error && <p className="text-sm text-red-600">{error}</p>}
          <button
            type="submit"
            disabled={loading || (!bootstrap && !selectedAddr)}
            className="w-full bg-blue-600 text-white py-3 rounded-lg text-sm font-medium hover:bg-blue-700 disabled:opacity-50"
          >
            {loading ? "Joining…" : "Join"}
          </button>
        </form>

        <div className="text-center">
          <button
            onClick={handleCreateCluster}
            disabled={loading}
            className="text-sm text-gray-500 hover:text-gray-700"
          >
            Or create a new pool
          </button>
        </div>
      </div>
    </div>
  );
}

export default function App() {
  const [tab, setTab] = useState("files");
  const [clusterId, setClusterId] = useState(null);
  const [discoveryMode, setDiscoveryMode] = useState("auto");
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    api("/node/info").then((r) => {
      setClusterId(r.data?.cluster_id || "");
      setDiscoveryMode(r.data?.discovery_mode || "auto");
      setLoading(false);
    });
  }, []);

  if (loading) return <div className="min-h-screen flex items-center justify-center text-gray-400">Loading…</div>;
  if (!clusterId) return <JoinPage mode={discoveryMode} />;

  return (
    <div className="min-h-screen pb-20">
      {/* Header with safe area */}
      <header
        className="bg-white border-b border-gray-200 px-4 py-3"
        style={{ paddingTop: 'max(0.75rem, env(safe-area-inset-top))' }}
      >
        <h1 className="text-lg font-bold text-center">PocketCluster</h1>
      </header>

      {/* Content */}
      <main className="p-4">
        {tab === "files" && <FilesPage />}
        {tab === "nodes" && <NodesPage />}
        {tab === "logs" && <LogsPage />}
      </main>

      {/* Bottom navigation */}
      <nav
        className="fixed bottom-0 left-0 right-0 bg-white border-t border-gray-200"
        style={{ paddingBottom: 'env(safe-area-inset-bottom)' }}
      >
        <div className="flex">
          {[
            { id: "files", label: "Files", icon: "📁" },
            { id: "nodes", label: "Nodes", icon: "🔗" },
            { id: "logs", label: "Logs", icon: "📋" },
          ].map((item) => (
            <button
              key={item.id}
              onClick={() => setTab(item.id)}
              className={`flex-1 py-3 text-center ${
                tab === item.id ? "text-blue-600" : "text-gray-500"
              }`}
            >
              <div className="text-lg">{item.icon}</div>
              <div className="text-xs font-medium">{item.label}</div>
            </button>
          ))}
        </div>
      </nav>
    </div>
  );
}

function formatBytes(bytes) {
  if (!bytes) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let i = 0;
  let v = bytes;
  while (v >= 1024 && i < units.length - 1) { v /= 1024; i++; }
  return `${v.toFixed(i > 0 ? 1 : 0)} ${units[i]}`;
}
