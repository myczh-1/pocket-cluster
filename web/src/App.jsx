import { useState, useEffect, useCallback } from "react";

const API = "/api";

async function api(path, opts) {
  const res = await fetch(`${API}${path}`, { ...opts, credentials: "same-origin" });
  const data = await res.json();
  data.status = res.status;
  return data;
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

function FileCard({ file, onDownload, onDelete, onRename }) {
  return (
    <div className={`bg-white rounded-lg shadow p-3 flex items-center gap-2 min-w-0 ${file.conflict_of ? "border-l-4 border-yellow-400" : ""}`}>
      <div className="w-7 h-7 rounded bg-gray-100 flex items-center justify-center text-xs font-medium text-gray-500 shrink-0">{file.is_dir ? "D" : "F"}</div>
      <div className="flex-1 min-w-0">
        <p className="font-medium text-sm truncate">{file.name}</p>
        <p className="text-xs text-gray-500 truncate">
          {file.is_dir ? "Directory" : formatBytes(file.size_bytes)}
          {file.modified_at && ` · ${new Date(file.modified_at).toLocaleDateString()}`}
        </p>
        {file.conflict_of && (
          <p className="text-xs text-yellow-600 mt-0.5">⚠ Conflict — original: {file.conflict_of}</p>
        )}
      </div>
      <div className="flex gap-1 shrink-0">
        {!file.is_dir && (
          <button
            onClick={() => onDownload(file)}
            className="px-2 py-1.5 bg-blue-50 text-blue-600 rounded text-xs font-medium hover:bg-blue-100 active:bg-blue-200"
          >
            Get
          </button>
        )}
        <button
          onClick={() => onRename(file)}
          className="px-2 py-1.5 bg-gray-50 text-gray-600 rounded text-xs font-medium hover:bg-gray-100 active:bg-gray-200"
        >
          Rename
        </button>
        <button
          onClick={() => onDelete(file)}
          className="px-2 py-1.5 bg-red-50 text-red-600 rounded text-xs font-medium hover:bg-red-100 active:bg-red-200"
        >
          Del
        </button>
      </div>
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
      const res = await fetch(`${API}/files/upload`, { method: "POST", body: formData, credentials: "same-origin" });
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
    window.location.assign(`${API}/files/download?path=${encodeURIComponent(file.path)}`);
  };

  const handleDelete = async (file) => {
    if (!confirm(`Delete "${file.name}"?`)) return;
    await fetch(`${API}/files?path=${encodeURIComponent(file.path)}`, { method: "DELETE" });
    loadFiles();
  };
  const handleRename = async (file) => {
    const newPath = prompt(`Rename "${file.name}" to:`, file.path);
    if (!newPath || newPath === file.path) return;
    const res = await fetch(`${API}/files/rename`, {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ path: file.path, new_path: newPath }),
    });
    if (!res.ok) {
      const err = await res.json().catch(() => ({}));
      alert(`Rename failed: ${err.error?.message || res.statusText}`);
    }
    loadFiles();
  };
  return (
    <div className="space-y-4">
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
          {refreshing ? "..." : "Refresh"}
        </button>
      </div>

      {/* Upload button */}
      <label className="block w-full mb-4">
        <div className="bg-blue-600 text-white text-center py-3 rounded-lg font-medium cursor-pointer hover:bg-blue-700 active:bg-blue-800">
          {uploading ? "Uploading..." : "Upload File"}
        </div>
        <input type="file" className="hidden" onChange={handleUpload} disabled={uploading} />
      </label>

      {/* File list */}
      <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
        {files.map((f) => (
          <FileCard key={f.file_id || f.path} file={f} onDownload={handleDownload} onDelete={handleDelete} onRename={handleRename} />
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
  const [pendingJoins, setPendingJoins] = useState([]);
  const [invite, setInvite] = useState(null);
  const [creatingInvite, setCreatingInvite] = useState(false);
  const [showSwitch, setShowSwitch] = useState(false);
  const [switchAddr, setSwitchAddr] = useState("");
  const [switchUser, setSwitchUser] = useState("");
  const [switchPass, setSwitchPass] = useState("");
  const [switchToken, setSwitchToken] = useState("");
  const [switching, setSwitching] = useState(false);
  const [switchError, setSwitchError] = useState(null);
  const [scanning, setScanning] = useState(false);

  const handleSwitch = async (e) => {
    e.preventDefault();
    if (!switchUser || !switchPass) { setSwitchError("Pool username and password are required"); return; }
    setSwitchError(null);
    setSwitching(true);
    try {
      const res = await api("/join", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ bootstrap: switchAddr, pool_user: switchUser, pool_password: switchPass, join_token: switchToken }),
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
    api("/join/pending").then((r) => { if (r.ok) setPendingJoins(r.data || []); });
  }, []);

  const approveJoin = async (nodeId) => {
    try {
      const r = await api(`/join/approve/${nodeId}`, { method: "POST" });
      if (r.ok) loadNodes();
    } catch (e) {
      console.error("Approve failed:", e);
    }
  };

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
    <div className="space-y-4">
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

      {/* Pending joins */}
      {pendingJoins.length > 0 && (
        <div className="bg-white rounded-lg shadow p-4 mb-4">
          <h2 className="font-semibold text-sm mb-3">Pending join requests</h2>
          <div className="space-y-2">
            {pendingJoins.map((pj) => (
              <div key={pj.node_id} className="flex items-center justify-between bg-gray-50 rounded-lg p-3">
                <div>
                  <p className="font-medium text-sm">{pj.name || pj.node_id.slice(0, 8)}</p>
                  <p className="text-xs text-gray-500">{pj.platform} / {pj.address}</p>
                </div>
                <button
                  onClick={() => approveJoin(pj.node_id)}
                  className="px-3 py-1.5 bg-green-600 text-white rounded-lg text-xs font-medium hover:bg-green-700"
                >
                  Approve
                </button>
              </div>
            ))}
          </div>
        </div>
      )}

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

      {/* Network scan */}
      <div className="bg-white rounded-lg shadow p-4 mb-4">
        <div className="flex items-center justify-between gap-3">
          <div>
            <h2 className="font-semibold text-sm">Scan local network</h2>
            <p className="text-xs text-gray-500">Find PocketCluster nodes on your network</p>
          </div>
          <button
            onClick={async () => {
              setScanning(true);
              try {
                const r = await api("/network/scan");
                if (r.ok && r.data?.nodes?.length > 0) {
                  const firstNode = r.data.nodes[0];
                  setSwitchAddr(`http://${firstNode.address}`);
                  setShowSwitch(true);
                }
              } catch (e) {
                console.error("Scan failed:", e);
              } finally {
                setScanning(false);
              }
            }}
            disabled={scanning}
            className="px-4 py-2 bg-gray-100 rounded-lg text-sm font-medium hover:bg-gray-200 disabled:opacity-50"
          >
            {scanning ? "Scanning..." : "Scan"}
          </button>
        </div>
      </div>

      {/* Switch pool */}
      <div className="bg-white rounded-lg shadow p-4 mb-4">
        <button
          onClick={() => setShowSwitch(!showSwitch)}
          className="text-sm text-gray-500 hover:text-gray-700 w-full text-left"
        >
          {showSwitch ? "Hide" : "Join another pool"}...
        </button>
        {showSwitch && (
          <form onSubmit={handleSwitch} className="mt-3 space-y-3">
            <input
              type="text"
              value={switchAddr}
              onChange={(e) => setSwitchAddr(e.target.value)}
              placeholder="Pool address (e.g. http://192.168.1.10:7788)"
              required
              className="w-full border rounded-lg px-4 py-3 text-sm"
            />
            <input
              type="text"
              value={switchUser}
              onChange={(e) => setSwitchUser(e.target.value)}
              placeholder="Pool username"
              required
              className="w-full border rounded-lg px-4 py-3 text-sm"
            />
            <input
              type="password"
              value={switchPass}
              onChange={(e) => setSwitchPass(e.target.value)}
              placeholder="Pool password"
              required
              className="w-full border rounded-lg px-4 py-3 text-sm"
            />
            <input
              type="text"
              value={switchToken}
              onChange={(e) => setSwitchToken(e.target.value)}
              placeholder="Invite token (optional)"
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
      <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
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
          Refresh
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

function LocalFilesPage() {
  const [cwd, setCwd] = useState("");
  const [parent, setParent] = useState("");
  const [entries, setEntries] = useState([]);
  const [migrating, setMigrating] = useState(null);
  const [targetPath, setTargetPath] = useState("");
  const [deleteLocal, setDeleteLocal] = useState(false);
  const [result, setResult] = useState(null);
  const [busy, setBusy] = useState(false);

  const load = useCallback(async (path) => {
    const q = path ? `?path=${encodeURIComponent(path)}` : "";
    const res = await api(`/local/files${q}`);
    if (res.ok) {
      setCwd(res.data.cwd);
      setParent(res.data.parent);
      setEntries(res.data.entries || []);
    }
  }, []);

  useEffect(() => { load(); }, [load]);

  const handleMigrate = async () => {
    setBusy(true);
    setResult(null);
    try {
      const res = await fetch(`${API}/local/migrate`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        credentials: "same-origin",
        body: JSON.stringify({
          path: migrating.path,
          target_path: targetPath || `/${migrating.name}`,
          delete_local: deleteLocal,
        }),
      });
      const data = await res.json();
      setResult(data);
      if (data.ok) {
        setTimeout(() => { setMigrating(null); setResult(null); }, 2000);
      }
    } catch (e) {
      setResult({ ok: false, error: { message: e.message } });
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="space-y-4">
      {/* Breadcrumb */}
      <div className="flex items-center gap-2 text-sm text-gray-500 overflow-x-auto">
        {cwd.split("/").filter(Boolean).map((seg, i, arr) => {
          const p = "/" + arr.slice(0, i + 1).join("/");
          return (
            <span key={p} className="flex items-center gap-2 shrink-0">
              <span className="text-gray-300">/</span>
              <button onClick={() => load(p)} className="hover:text-blue-600 truncate max-w-[120px]">{seg}</button>
            </span>
          );
        })}
      </div>

      {/* Up button */}
      {parent && parent !== cwd && (
        <button
          onClick={() => load(parent)}
          className="px-3 py-2 bg-gray-100 rounded-lg text-sm hover:bg-gray-200"
        >
          Parent
        </button>
      )}

      {/* File list */}
      <div className="grid gap-2 md:grid-cols-2 xl:grid-cols-3">
        {entries.map((e) => (
          <div key={e.path} className="bg-white rounded-lg shadow p-3 flex items-center gap-3">
            <span className="w-8 h-8 rounded bg-gray-100 flex items-center justify-center text-xs font-medium text-gray-500">{e.is_dir ? "D" : "F"}</span>
            <div className="flex-1 min-w-0">
              {e.is_dir ? (
                <button onClick={() => load(e.path)} className="font-medium text-sm text-blue-600 hover:underline truncate block w-full text-left">
                  {e.name}
                </button>
              ) : (
                <p className="font-medium text-sm truncate">{e.name}</p>
              )}
              <p className="text-xs text-gray-400">{e.is_dir ? "Directory" : formatBytes(e.size_bytes)}</p>
            </div>
            {!e.is_dir && (
              <button
                onClick={() => { setMigrating(e); setTargetPath("/" + e.name); setDeleteLocal(false); setResult(null); }}
                className="px-3 py-1.5 bg-green-50 text-green-700 rounded-lg text-xs font-medium hover:bg-green-100"
              >
                Migrate → Pool
              </button>
            )}
          </div>
        ))}
        {entries.length === 0 && (
          <div className="bg-white rounded-lg shadow p-8 text-center text-gray-400 text-sm col-span-full">
            Empty directory
          </div>
        )}
      </div>

      {/* Migrate modal */}
      {migrating && (
        <div className="fixed inset-0 bg-black/30 flex items-center justify-center z-50 p-4">
          <div className="bg-white rounded-xl shadow-xl w-full max-w-md p-6 space-y-4">
            <h3 className="font-semibold text-base">Migrate to Pool</h3>
            <p className="text-sm text-gray-500">{migrating.name} ({formatBytes(migrating.size_bytes)})</p>
            <label className="block">
              <span className="text-xs text-gray-500 mb-1 block">Target path in pool</span>
              <input
                type="text"
                value={targetPath}
                onChange={(e) => setTargetPath(e.target.value)}
                className="w-full border rounded-lg px-3 py-2 text-sm"
              />
            </label>
            <label className="flex items-center gap-2 text-sm">
              <input type="checkbox" checked={deleteLocal} onChange={(e) => setDeleteLocal(e.target.checked)} />
              <span>Delete local file after migration</span>
            </label>
            {result && (
              <div className={`text-sm p-3 rounded-lg ${result.ok ? "bg-green-50 text-green-700" : "bg-red-50 text-red-700"}`}>
                {result.ok ? `Migrated! ${result.data?.chunk_count} chunks, status: ${result.data?.replica_status}` : result.error?.message}
              </div>
            )}
            <div className="flex gap-2 justify-end">
              <button onClick={() => { setMigrating(null); setResult(null); }} className="px-4 py-2 text-sm text-gray-600 hover:bg-gray-100 rounded-lg">Cancel</button>
              <button onClick={handleMigrate} disabled={busy} className="px-4 py-2 text-sm bg-blue-600 text-white rounded-lg hover:bg-blue-700 disabled:opacity-50">
                {busy ? "Migrating…" : "Migrate"}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

function JoinPage({ mode }) {
  const [bootstrap, setBootstrap] = useState("");
  const [token, setToken] = useState("");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState(null);
  const [discovered, setDiscovered] = useState([]);
  const [selectedAddr, setSelectedAddr] = useState("");
  const [scanning, setScanning] = useState(false);
  const [scanResults, setScanResults] = useState([]);
  const [showCreate, setShowCreate] = useState(false);

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
  const handleCreateCluster = async (e) => {
    e.preventDefault();
    if (!username || !password) { setError("Username and password are required"); return; }
    setLoading(true);
    setError(null);
    try {
      const r = await api("/cluster", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ username, password }),
      });
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
    if (!username || !password) { setError("Pool username and password are required"); return; }
    setLoading(true);
    setError(null);
    try {
      const addr = selectedAddr || bootstrap;
      const r = await api("/join", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ bootstrap: addr, join_token: token, pool_user: username, pool_password: password }),
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
              {scanning ? "Scanning..." : "Scan"}
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
            placeholder="Pool address (e.g. http://192.168.1.10:7788)"
            className="w-full border rounded-lg px-4 py-3 text-sm"
          />
          <input
            type="text"
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            placeholder="Pool username"
            required
            className="w-full border rounded-lg px-4 py-3 text-sm"
          />
          <input
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            placeholder="Pool password"
            required
            className="w-full border rounded-lg px-4 py-3 text-sm"
          />
          <input
            type="text"
            value={token}
            onChange={(e) => setToken(e.target.value)}
            placeholder="Invite token (optional)"
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
        <div className="bg-white rounded-lg shadow p-4">
          <button
            onClick={() => setShowCreate(!showCreate)}
            className="text-sm text-gray-500 hover:text-gray-700 w-full text-left"
          >
            {showCreate ? "Hide" : "Create a new pool"}...
          </button>
          {showCreate && (
            <form onSubmit={handleCreateCluster} className="mt-3 space-y-3">
              <input
                type="text"
                value={username}
                onChange={(e) => setUsername(e.target.value)}
                placeholder="Pool username"
                required
                className="w-full border rounded-lg px-4 py-3 text-sm"
              />
              <input
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                placeholder="Pool password"
                required
                className="w-full border rounded-lg px-4 py-3 text-sm"
              />
              {error && <p className="text-sm text-red-600">{error}</p>}
              <button
                type="submit"
                disabled={loading || !username || !password}
                className="w-full bg-blue-600 text-white py-3 rounded-lg text-sm font-medium hover:bg-blue-700 disabled:opacity-50"
              >
                {loading ? "Creating..." : "Create Pool"}
              </button>
            </form>
          )}
        </div>
      </div>
    </div>
  );
}
function LoginPage() {
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState(null);
  const [busy, setBusy] = useState(false);

  const handleSubmit = async (e) => {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const res = await fetch(`${API}/auth/login`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        credentials: "same-origin",
        body: JSON.stringify({ username, password }),
      });
      const data = await res.json();
      if (data.ok) {
        window.location.reload();
      } else {
        setError(data.error?.message || "Login failed");
      }
    } catch (err) {
      setError(err.message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="min-h-screen flex items-center justify-center p-4">
      <div className="w-full max-w-sm">
        <div className="text-center mb-8">
          <h1 className="text-2xl font-bold mb-2">PocketCluster</h1>
          <p className="text-gray-500 text-sm">Login to your storage pool</p>
        </div>
        <form onSubmit={handleSubmit} className="bg-white rounded-lg shadow p-4 space-y-3">
          <input
            type="text"
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            placeholder="Username"
            required
            className="w-full border rounded-lg px-4 py-3 text-sm"
          />
          <input
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            placeholder="Password"
            required
            className="w-full border rounded-lg px-4 py-3 text-sm"
          />
          {error && <p className="text-sm text-red-600">{error}</p>}
          <button
            type="submit"
            disabled={busy || !username || !password}
            className="w-full bg-blue-600 text-white py-3 rounded-lg text-sm font-medium hover:bg-blue-700 disabled:opacity-50"
          >
            {busy ? "Logging in..." : "Login"}
          </button>
        </form>
      </div>
    </div>
  );
}

function HealthPage() {
  const [summary, setSummary] = useState(null);
  const [chunks, setChunks] = useState([]);
  const [selectedChunk, setSelectedChunk] = useState(null);
  const [loading, setLoading] = useState(true);
  const [page, setPage] = useState(0);
  const pageSize = 100;
  const load = useCallback(async () => {
    setLoading(true);
    const [sumRes, chunkRes] = await Promise.all([
      api("/health/summary"),
      api(`/health/chunks?limit=${pageSize}&offset=${page * pageSize}`),
    ]);
    if (sumRes.ok) setSummary(sumRes.data);
    if (chunkRes.ok) setChunks(chunkRes.data?.chunks || []);
    setLoading(false);
  }, [page]);
  useEffect(() => { load(); }, [load]);
  if (loading) return <div className="text-center text-gray-400 py-8">Loading health data...</div>;
  if (!summary) return <div className="text-center text-gray-400 py-8">Health data unavailable</div>;
  const statusColor = {
    healthy: "text-green-600 bg-green-50",
    under_replicated: "text-yellow-600 bg-yellow-50",
    unavailable: "text-red-600 bg-red-50",
    repairing: "text-blue-600 bg-blue-50",
  };
  return (
    <div className="space-y-6">
      {/* Summary cards */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
        <div className={`rounded-lg p-4 ${statusColor[summary.overall_status] || "bg-gray-50"}`}>
          <div className="text-xs font-medium uppercase opacity-70">Overall</div>
          <div className="text-lg font-bold mt-1">{summary.overall_status}</div>
        </div>
        <div className="bg-white rounded-lg shadow p-4">
          <div className="text-xs font-medium text-gray-500 uppercase">Files</div>
          <div className="text-lg font-bold mt-1">{summary.total_files}</div>
        </div>
        <div className="bg-white rounded-lg shadow p-4">
          <div className="text-xs font-medium text-gray-500 uppercase">Chunks</div>
          <div className="text-lg font-bold mt-1">{summary.total_chunks}</div>
        </div>
        <div className="bg-white rounded-lg shadow p-4">
          <div className="text-xs font-medium text-gray-500 uppercase">Healthy</div>
          <div className="text-lg font-bold mt-1 text-green-600">{summary.healthy_chunks}</div>
        </div>
      </div>
      {/* Detail stats */}
      <div className="grid grid-cols-3 gap-3">
        <div className="bg-white rounded-lg shadow p-4">
          <div className="text-xs font-medium text-gray-500 uppercase">Under-replicated</div>
          <div className={`text-lg font-bold mt-1 ${summary.under_replicated_chunks > 0 ? "text-yellow-600" : "text-gray-400"}`}>
            {summary.under_replicated_chunks}
          </div>
        </div>
        <div className="bg-white rounded-lg shadow p-4">
          <div className="text-xs font-medium text-gray-500 uppercase">Unavailable</div>
          <div className={`text-lg font-bold mt-1 ${summary.unavailable_chunks > 0 ? "text-red-600" : "text-gray-400"}`}>
            {summary.unavailable_chunks}
          </div>
        </div>
        <div className="bg-white rounded-lg shadow p-4">
          <div className="text-xs font-medium text-gray-500 uppercase">Repairing</div>
          <div className={`text-lg font-bold mt-1 ${summary.repairing_chunks > 0 ? "text-blue-600" : "text-gray-400"}`}>
            {summary.repairing_chunks}
          </div>
        </div>
      </div>
      {/* Scan info */}
      <div className="text-xs text-gray-400">
        Last scan: {summary.last_scan_at ? new Date(summary.last_scan_at).toLocaleString() : "never"}
        {summary.last_repair_at && <> · Last repair: {new Date(summary.last_repair_at).toLocaleString()}</>}
      </div>
      {/* Chunk detail */}
      {selectedChunk && (
        <div className="bg-white rounded-lg shadow p-4">
          <div className="flex justify-between items-center mb-3">
            <h3 className="font-medium text-sm">Chunk Detail</h3>
            <button onClick={() => setSelectedChunk(null)} className="text-gray-400 hover:text-gray-600 text-sm">✕</button>
          </div>
          <div className="text-xs font-mono text-gray-500 mb-2 break-all">{selectedChunk.chunk_id}</div>
          <div className="grid grid-cols-2 gap-2 text-sm mb-3">
            <div>Size: {formatBytes(selectedChunk.size_bytes)}</div>
            <div>Replicas: {selectedChunk.online_replicas}/{selectedChunk.target_replicas}</div>
            <div>Status: <span className={`font-medium ${selectedChunk.status === "healthy" ? "text-green-600" : selectedChunk.status === "unavailable" ? "text-red-600" : "text-yellow-600"}`}>{selectedChunk.status}</span></div>
          </div>
          {selectedChunk.replica_nodes && selectedChunk.replica_nodes.length > 0 && (
            <div>
              <div className="text-xs font-medium text-gray-500 mb-1">Replica Nodes</div>
              <div className="space-y-1">
                {selectedChunk.replica_nodes.map((r) => (
                  <div key={r.node_id} className="flex items-center gap-2 text-xs">
                    <span className={`w-2 h-2 rounded-full ${r.online ? "bg-green-500" : "bg-gray-300"}`}></span>
                    <span className="font-mono">{r.node_id}</span>
                    <span className="text-gray-400">{r.online ? "online" : "offline"}</span>
                  </div>
                ))}
              </div>
            </div>
          )}
          {selectedChunk.referencing_files && selectedChunk.referencing_files.length > 0 && (
            <div className="mt-3">
              <div className="text-xs font-medium text-gray-500 mb-1">Referencing Files</div>
              <div className="space-y-1">
                {selectedChunk.referencing_files.map((p) => (
                  <div key={p} className="text-xs font-mono text-gray-600">{p}</div>
                ))}
              </div>
            </div>
          )}
        </div>
      )}
      {/* Chunk list */}
      <div>
        <h3 className="font-medium text-sm mb-3">All Chunks ({chunks.length})</h3>
        <div className="space-y-2">
          {chunks.map((c) => (
            <button
              key={c.chunk_id}
              onClick={() => setSelectedChunk(c)}
              className={`w-full text-left bg-white rounded-lg shadow p-3 hover:shadow-md transition-shadow ${
                selectedChunk?.chunk_id === c.chunk_id ? "ring-2 ring-blue-500" : ""
              }`}
            >
              <div className="flex items-center justify-between">
                <div className="font-mono text-xs text-gray-500 truncate max-w-[200px]">{c.chunk_id}</div>
                <span className={`inline-block px-2 py-0.5 rounded text-xs font-medium ${
                  c.status === "healthy" ? "bg-green-100 text-green-700" :
                  c.status === "unavailable" ? "bg-red-100 text-red-700" :
                  c.status === "repairing" ? "bg-blue-100 text-blue-700" :
                  "bg-yellow-100 text-yellow-700"
                }`}>
                  {c.status}
                </span>
              </div>
              <div className="flex gap-4 mt-1 text-xs text-gray-400">
                <span>{formatBytes(c.size_bytes)}</span>
                <span>{c.online_replicas}/{c.target_replicas} replicas</span>
                {c.referencing_files && <span>{c.referencing_files.length} file(s)</span>}
              </div>
            </button>
          ))}
          {chunks.length === 0 && (
            <div className="bg-white rounded-lg shadow p-8 text-center text-gray-400 text-sm">
              No chunks found
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
const navItems = [
  { id: "files", label: "Files" },
  { id: "nodes", label: "Nodes" },
  { id: "health", label: "Health" },
  { id: "logs", label: "Logs" },
];


export default function App() {
  const [tab, setTab] = useState("files");
  const [clusterId, setClusterId] = useState(null);
  const [discoveryMode, setDiscoveryMode] = useState("auto");
  const [loading, setLoading] = useState(true);
  const [needsLogin, setNeedsLogin] = useState(false);
  const [noCluster, setNoCluster] = useState(false);

  useEffect(() => {
    api("/auth/status").then((r) => {
      if (!r.ok) { setLoading(false); return; }
      const hasCreds = r.data?.has_credentials;
      if (!hasCreds) {
        setNoCluster(true);
        setLoading(false);
        return;
      }
      api("/node/info").then((r2) => {
        if (r2.ok) {
          setClusterId(r2.data?.cluster_id || "");
          setDiscoveryMode(r2.data?.discovery_mode || "auto");
        } else {
          setNeedsLogin(true);
        }
        setLoading(false);
      });
    });
  }, []);

  if (loading) return <div className="min-h-screen flex items-center justify-center text-gray-400">Loading...</div>;
  if (needsLogin) return <LoginPage />;
  if (noCluster || !clusterId) return <JoinPage mode={discoveryMode} />;

  return (
    <div className="min-h-screen lg:flex" style={{ paddingTop: 'env(safe-area-inset-top)' }}>
      <header className="bg-white border-b border-gray-200 px-4 py-3 lg:fixed lg:inset-y-0 lg:left-0 lg:w-64 lg:border-b-0 lg:border-r lg:px-6 lg:py-6">
        <h1 className="text-lg font-bold text-center lg:text-left lg:text-2xl">PocketCluster</h1>
        <nav className="mt-8 hidden lg:block">
          <div className="space-y-1">
            {navItems.map((item) => (
              <button
                key={item.id}
                onClick={() => setTab(item.id)}
                className={`flex w-full items-center rounded-xl px-3 py-2.5 text-left text-sm font-medium ${
                  tab === item.id ? "bg-blue-50 text-blue-700" : "text-gray-600 hover:bg-gray-100 hover:text-gray-900"
                }`}
              >
                {item.label}
              </button>
            ))}
          </div>
        </nav>
      </header>

      <main className="p-4 pb-28 lg:ml-64 lg:flex-1 lg:p-8 xl:p-10">
        <div className="mx-auto w-full max-w-7xl">
          {tab === "files" && <FilesPage />}
          {tab === "nodes" && <NodesPage />}
          {tab === "health" && <HealthPage />}
          {tab === "logs" && <LogsPage />}
        </div>
      </main>

      <nav
        className="fixed bottom-0 left-0 right-0 bg-white border-t border-gray-200 lg:hidden z-50"
        style={{ paddingBottom: 'max(0.5rem, env(safe-area-inset-bottom))' }}
      >
        <div className="flex">
          {navItems.map((item) => (
            <button
              key={item.id}
              onClick={() => setTab(item.id)}
              className={`flex-1 py-3 text-center ${
                tab === item.id ? "text-blue-600" : "text-gray-500"
              }`}
            >
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
