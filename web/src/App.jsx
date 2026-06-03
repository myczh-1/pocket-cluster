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

function FileRow({ file, onDownload, onDelete }) {
  return (
    <tr className="border-b border-gray-100 hover:bg-gray-50">
      <td className="py-2 px-3 text-sm">
        {file.is_dir ? "📁" : "📄"} {file.name}
      </td>
      <td className="py-2 px-3 text-sm text-gray-500">{file.is_dir ? "—" : formatBytes(file.size_bytes)}</td>
      <td className="py-2 px-3 text-sm text-gray-500">{file.modified_at ? new Date(file.modified_at).toLocaleDateString() : "—"}</td>
      <td className="py-2 px-3 text-right space-x-2">
        {!file.is_dir && (
          <button onClick={() => onDownload(file)} className="text-blue-600 hover:underline text-xs">Download</button>
        )}
        {!file.is_dir && (
          <button onClick={() => onDelete(file)} className="text-red-600 hover:underline text-xs">Delete</button>
        )}
      </td>
    </tr>
  );
}

function FilesPage() {
  const [path, setPath] = useState("/");
  const [files, setFiles] = useState([]);
  const [search, setSearch] = useState("");
  const [uploading, setUploading] = useState(false);

  const loadFiles = useCallback(async () => {
    const q = search ? `?q=${encodeURIComponent(search)}` : `?path=${encodeURIComponent(path)}`;
    const res = await api(`/files${q}`);
    if (res.ok) setFiles(res.data.entries || []);
  }, [path, search]);

  useEffect(() => { loadFiles(); }, [loadFiles]);

  const handleUpload = async (e) => {
    const file = e.target.files[0];
    if (!file) return;
    setUploading(true);
    const formData = new FormData();
    formData.append("path", path === "/" ? `/${file.name}` : `${path}/${file.name}`);
    formData.append("file", file);
    await fetch(`${API}/files/upload`, { method: "POST", body: formData });
    setUploading(false);
    loadFiles();
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
      <div className="flex items-center gap-3 mb-4">
        <input
          type="text"
          placeholder="Search files…"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          className="border rounded px-3 py-1.5 text-sm flex-1 max-w-xs"
        />
        <button
          onClick={loadFiles}
          className="bg-gray-200 text-gray-700 px-3 py-1.5 rounded text-sm hover:bg-gray-300"
        >
          ↻ Refresh
        </button>
        <label className="bg-blue-600 text-white px-4 py-1.5 rounded text-sm cursor-pointer hover:bg-blue-700">
          {uploading ? "Uploading…" : "Upload"}
          <input type="file" className="hidden" onChange={handleUpload} disabled={uploading} />
        </label>
      </div>
      <div className="bg-white rounded-lg shadow overflow-hidden">
        <table className="w-full">
          <thead className="bg-gray-50 text-xs text-gray-500 uppercase">
            <tr>
              <th className="text-left py-2 px-3">Name</th>
              <th className="text-left py-2 px-3">Size</th>
              <th className="text-left py-2 px-3">Modified</th>
              <th className="text-right py-2 px-3">Action</th>
            </tr>
          </thead>
          <tbody>
            {files.map((f) => (
              <FileRow key={f.file_id || f.path} file={f} onDownload={handleDownload} onDelete={handleDelete} />
            ))}
            {files.length === 0 && (
              <tr><td colSpan={4} className="py-8 text-center text-gray-400 text-sm">No files</td></tr>
            )}
          </tbody>
        </table>
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
      <div className="grid grid-cols-3 gap-4 mb-6">
        <div className="bg-white rounded-lg shadow p-4 text-center">
          <p className="text-2xl font-bold text-blue-600">{nodes.length}</p>
          <p className="text-xs text-gray-500">Nodes</p>
        </div>
        <div className="bg-white rounded-lg shadow p-4 text-center">
          <p className="text-2xl font-bold text-green-600">{onlineCount}</p>
          <p className="text-xs text-gray-500">Online</p>
        </div>
        <div className="bg-white rounded-lg shadow p-4 text-center">
          <p className="text-2xl font-bold">{formatBytes(totalBytes)}</p>
          <p className="text-xs text-gray-500">Total · {formatBytes(usedBytes)} used</p>
        </div>
      </div>
      <div className="bg-white rounded-lg shadow p-4 mb-6">
        <div className="flex items-center justify-between gap-4">
          <div>
            <h2 className="font-semibold text-sm">Invite a node</h2>
            <p className="text-xs text-gray-500">Creates a one-time token that expires in 15 minutes.</p>
          </div>
          <button
            onClick={createInvite}
            disabled={creatingInvite}
            className="bg-blue-600 text-white px-4 py-1.5 rounded text-sm hover:bg-blue-700 disabled:opacity-50"
          >
            {creatingInvite ? "Creating…" : "Create invite"}
          </button>
        </div>
        {invite && (
          <div className="mt-4 bg-gray-50 rounded p-3">
            <p className="text-xs text-gray-500 mb-1">Join token</p>
            <code className="block text-sm break-all">{invite.join_token}</code>
            <p className="text-xs text-gray-500 mt-3 mb-1">New node: open its UI and paste the bootstrap address + token</p>
            <code className="block text-xs break-all text-gray-600">{window.location.origin}</code>
            <p className="text-xs text-gray-400 mt-2">Expires {new Date(invite.expires_at).toLocaleString()}</p>
          </div>
        )}
      </div>
      <div className="bg-white rounded-lg shadow p-4 mb-6">
        <button
          onClick={() => setShowSwitch(!showSwitch)}
          className="text-sm text-gray-500 hover:text-gray-700"
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
              className="w-full border rounded px-3 py-2 text-sm"
            />
            <input
              type="text"
              value={switchToken}
              onChange={(e) => setSwitchToken(e.target.value)}
              placeholder="Invite token (leave empty for auto mode)"
              className="w-full border rounded px-3 py-2 text-sm"
            />
            {switchError && <p className="text-sm text-red-600">{switchError}</p>}
            <button
              type="submit"
              disabled={switching || !switchAddr}
              className="bg-blue-600 text-white px-4 py-1.5 rounded text-sm hover:bg-blue-700 disabled:opacity-50"
            >
              {switching ? "Joining…" : "Switch pool"}
            </button>
          </form>
        )}
      </div>
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        {nodes.map((n) => <NodeCard key={n.node_id} node={n} />)}
      </div>
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

  useEffect(() => {
    if (mode === "invite") {
      const poll = () => api("/nodes/discovered").then((r) => { if (r.ok) setDiscovered(r.data || []); });
      poll();
      const id = setInterval(poll, 3000);
      return () => clearInterval(id);
    }
  }, [mode]);

  const handleCreate = async () => {
    setError(null);
    setLoading(true);
    try {
      const res = await api("/cluster", { method: "POST" });
      if (res.ok) window.location.reload();
      else setError(res.error?.message || "Create failed");
    } catch (err) {
      setError(err.message || "Network error");
    } finally {
      setLoading(false);
    }
  };

  const handleJoin = async (e) => {
    e.preventDefault();
    setError(null);
    setLoading(true);
    const addr = selectedAddr || bootstrap;
    try {
      const res = await api("/join", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ bootstrap: addr, join_token: token }),
      });
      if (res.ok) window.location.reload();
      else setError(res.error?.message || "Join failed");
    } catch (err) {
      setError(err.message || "Network error");
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="min-h-screen flex items-center justify-center">
      <div className="bg-white rounded-lg shadow p-8 w-full max-w-md">
        <h1 className="text-xl font-bold mb-2">PocketCluster</h1>
        <p className="text-sm text-gray-500 mb-6">This node is not part of a pool yet.</p>

        {mode === "auto" && (
          <div className="mb-6 bg-blue-50 rounded p-3 text-sm text-blue-700 flex items-center gap-2">
            <span className="animate-pulse">●</span>
            Auto-discovering peers on the local network…
          </div>
        )}

        <div className="mb-6">
          <h2 className="text-sm font-semibold text-gray-700 mb-3">First node?</h2>
          <button
            onClick={handleCreate}
            disabled={loading}
            className="w-full bg-green-600 text-white rounded px-4 py-2 text-sm hover:bg-green-700 disabled:opacity-50"
          >
            {loading ? "Creating…" : "Create new pool"}
          </button>
        </div>

        <div className="border-t pt-6">
          <h2 className="text-sm font-semibold text-gray-700 mb-3">Join an existing pool</h2>
          {mode === "invite" && discovered.length > 0 && (
            <div className="mb-3">
              <label className="block text-xs text-gray-500 mb-1">Discovered nodes</label>
              <select
                value={selectedAddr}
                onChange={(e) => setSelectedAddr(e.target.value)}
                className="w-full border rounded px-3 py-2 text-sm"
              >
                <option value="">Select a node…</option>
                {discovered.map((n) => (
                  <option key={n.node_id} value={"http://" + n.address}>{n.name} ({n.address})</option>
                ))}
              </select>
            </div>
          )}
          <form onSubmit={handleJoin} className="space-y-3">
            {!selectedAddr && (
              <div>
                <label className="block text-xs text-gray-500 mb-1">Bootstrap node address</label>
                <input
                  type="text"
                  value={bootstrap}
                  onChange={(e) => setBootstrap(e.target.value)}
                  placeholder="http://192.168.1.10:7788"
                  className="w-full border rounded px-3 py-2 text-sm"
                />
              </div>
            )}
            {mode === "invite" && (
              <div>
                <label className="block text-xs text-gray-500 mb-1">Invite token</label>
                <input
                  type="text"
                  value={token}
                  onChange={(e) => setToken(e.target.value)}
                  placeholder="Paste invite token"
                  className="w-full border rounded px-3 py-2 text-sm"
                />
              </div>
            )}
            <button
              type="submit"
              disabled={loading || !(selectedAddr || bootstrap) || (mode === "invite" && !token)}
              className="w-full bg-blue-600 text-white rounded px-4 py-2 text-sm hover:bg-blue-700 disabled:opacity-50"
            >
              {loading ? "Joining…" : "Join pool"}
            </button>
          </form>
        </div>

        {error && <p className="text-sm text-red-600 mt-4">{error}</p>}
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
    <div className="min-h-screen">
      <header className="bg-white border-b border-gray-200 px-6 py-3 flex items-center justify-between" style={{ paddingTop: 'max(0.75rem, env(safe-area-inset-top))' }}>
        <h1 className="text-lg font-bold">PocketCluster</h1>
        <nav className="flex gap-4">
          {["files", "nodes"].map((t) => (
            <button
              key={t}
              onClick={() => setTab(t)}
              className={`text-sm capitalize ${tab === t ? "text-blue-600 font-semibold" : "text-gray-500 hover:text-gray-800"}`}
            >
              {t}
            </button>
          ))}
        </nav>
      </header>
      <main className="max-w-5xl mx-auto p-6">
        {tab === "files" && <FilesPage />}
        {tab === "nodes" && <NodesPage />}
      </main>
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
