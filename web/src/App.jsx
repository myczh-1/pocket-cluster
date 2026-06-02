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
      <div className="w-full bg-gray-200 rounded-full h-2">
        <div className="bg-blue-600 h-2 rounded-full" style={{ width: `${usedPct}%` }} />
      </div>
      <p className="text-xs text-gray-400 mt-1">{formatBytes(node.used_bytes)} / {formatBytes(node.total_bytes)}</p>
    </div>
  );
}

function FileRow({ file, onDownload }) {
  return (
    <tr className="border-b border-gray-100 hover:bg-gray-50">
      <td className="py-2 px-3 text-sm">
        {file.is_dir ? "📁" : "📄"} {file.name}
      </td>
      <td className="py-2 px-3 text-sm text-gray-500">{file.is_dir ? "—" : formatBytes(file.size_bytes)}</td>
      <td className="py-2 px-3 text-sm text-gray-500">{file.modified_at ? new Date(file.modified_at).toLocaleDateString() : "—"}</td>
      <td className="py-2 px-3 text-right">
        {!file.is_dir && (
          <button onClick={() => onDownload(file)} className="text-blue-600 hover:underline text-xs">Download</button>
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
              <FileRow key={f.file_id || f.path} file={f} onDownload={handleDownload} />
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
  useEffect(() => {
    api("/nodes").then((r) => { if (r.ok) setNodes(r.data || []); });
  }, []);
  const totalBytes = nodes.reduce((s, n) => s + (n.total_bytes || 0), 0);
  const usedBytes = nodes.reduce((s, n) => s + (n.used_bytes || 0), 0);
  const onlineCount = nodes.filter((n) => n.status === "online").length;

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
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        {nodes.map((n) => <NodeCard key={n.node_id} node={n} />)}
      </div>
    </div>
  );
}

export default function App() {
  const [tab, setTab] = useState("files");
  return (
    <div className="min-h-screen">
      <header className="bg-white border-b border-gray-200 px-6 py-3 flex items-center justify-between">
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
