import { useState, useEffect, useCallback } from "react";

const API = "/api";

async function api(path, opts) {
  const res = await fetch(`${API}${path}`, { ...opts, credentials: "same-origin" });
  const text = await res.text();
  let data;
  try {
    data = text ? JSON.parse(text) : {};
  } catch {
    data = { ok: false, error: { message: `Unexpected response from ${path}` } };
  }
  data.status = res.status;
  return data;
}

function cx(...parts) {
  return parts.filter(Boolean).join(" ");
}

function StatusBadge({ status }) {
  const colors = {
    healthy: "border-green-200 bg-green-50 text-green-700",
    under_replicated: "border-amber-200 bg-amber-50 text-amber-700",
    unavailable: "border-red-200 bg-red-50 text-red-700",
    repairing: "border-blue-200 bg-blue-50 text-blue-700",
    online: "border-green-200 bg-green-50 text-green-700",
    offline: "border-slate-200 bg-slate-100 text-slate-600",
  };
  return (
    <span className={cx(
      "inline-flex items-center rounded-full border px-2.5 py-1 text-xs font-semibold capitalize",
      colors[status] || "border-slate-200 bg-slate-100 text-slate-600"
    )}>
      {status}
    </span>
  );
}

function PageHeader({ title, description, eyebrow, action }) {
  return (
    <div className="mb-5 flex flex-col gap-3 border-b border-slate-200 pb-5 sm:flex-row sm:items-end sm:justify-between">
      <div className="min-w-0">
        {eyebrow && <p className="text-xs font-semibold uppercase tracking-wide text-blue-700">{eyebrow}</p>}
        <h2 className="mt-1 text-2xl font-semibold text-slate-950">{title}</h2>
        {description && <p className="mt-1 max-w-2xl text-sm leading-6 text-slate-500">{description}</p>}
      </div>
      {action && <div className="shrink-0">{action}</div>}
    </div>
  );
}

function Section({ title, description, action, children, className = "" }) {
  return (
    <section className={cx("rounded-lg border border-slate-200 bg-white shadow-sm", className)}>
      {(title || action) && (
        <div className="flex flex-col gap-3 border-b border-slate-100 px-4 py-3 sm:flex-row sm:items-center sm:justify-between">
          <div>
            {title && <h3 className="text-sm font-semibold text-slate-950">{title}</h3>}
            {description && <p className="mt-0.5 text-xs leading-5 text-slate-500">{description}</p>}
          </div>
          {action}
        </div>
      )}
      <div className="p-4">{children}</div>
    </section>
  );
}

function InlineMessage({ tone = "info", children }) {
  const styles = {
    info: "border-blue-200 bg-blue-50 text-blue-800",
    success: "border-green-200 bg-green-50 text-green-800",
    warning: "border-amber-200 bg-amber-50 text-amber-800",
    error: "border-red-200 bg-red-50 text-red-800",
  };
  return (
    <div className={cx("rounded-lg border px-3 py-2 text-sm", styles[tone] || styles.info)}>
      {children}
    </div>
  );
}

function EmptyState({ title, description, action }) {
  return (
    <div className="col-span-full rounded-lg border border-dashed border-slate-300 bg-slate-50 px-4 py-10 text-center">
      <p className="text-sm font-semibold text-slate-700">{title}</p>
      {description && <p className="mx-auto mt-1 max-w-md text-sm text-slate-500">{description}</p>}
      {action && <div className="mt-4">{action}</div>}
    </div>
  );
}

function ProgressBar({ value, tone = "blue" }) {
  const color = tone === "green" ? "bg-green-600" : tone === "amber" ? "bg-amber-500" : "bg-blue-600";
  return (
    <div className="h-2 w-full overflow-hidden rounded-full bg-slate-200">
      <div className={cx("h-full rounded-full transition-all", color)} style={{ width: `${Math.min(100, Math.max(0, value))}%` }} />
    </div>
  );
}

function ConfirmDialog({ title, message, confirmLabel = "Confirm", tone = "danger", busy, onConfirm, onCancel }) {
  const confirmClass = tone === "danger"
    ? "bg-red-600 text-white hover:bg-red-700"
    : "bg-blue-600 text-white hover:bg-blue-700";
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-950/45 p-4" onMouseDown={onCancel}>
      <div className="w-full max-w-md rounded-lg border border-slate-200 bg-white p-5 shadow-2xl" onMouseDown={(e) => e.stopPropagation()}>
        <h3 className="text-base font-semibold text-slate-950">{title}</h3>
        <p className="mt-2 text-sm leading-6 text-slate-600">{message}</p>
        <div className="mt-5 flex justify-end gap-2">
          <button type="button" onClick={onCancel} className="rounded-lg px-4 py-2 text-sm font-medium text-slate-600 hover:bg-slate-100">
            Cancel
          </button>
          <button type="button" onClick={onConfirm} disabled={busy} className={cx("rounded-lg px-4 py-2 text-sm font-semibold disabled:opacity-50", confirmClass)}>
            {busy ? "Working..." : confirmLabel}
          </button>
        </div>
      </div>
    </div>
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
    <div className="rounded-lg border border-slate-200 bg-white p-4 shadow-sm">
      <div className="mb-3 flex items-start justify-between gap-3">
        <div className="min-w-0">
          <h3 className="truncate text-sm font-semibold text-slate-950">{node.name || node.node_id.slice(0, 8)}</h3>
          <p className="mt-1 truncate text-xs text-slate-500">{node.platform} · {node.node_id.slice(0, 8)}</p>
        </div>
        <StatusBadge status={node.status} />
      </div>
      <div className="space-y-2 text-xs text-slate-500">
        <div className="flex items-center justify-between gap-3">
          <span className="truncate">{node.address}</span>
          <span className="shrink-0">Seen {formatLastSeen(node.last_seen)}</span>
        </div>
        <ProgressBar value={usedPct} />
        <div className="flex items-center justify-between">
          <span>{formatBytes(node.used_bytes)} used</span>
          <span>{formatBytes(node.total_bytes)} total</span>
        </div>
      </div>
    </div>
  );
}

function FileCard({ file, onDownload, onDelete, onRename, onPreview }) {
  const canPreview = !file.is_dir && file.mime_type && (
    file.mime_type.startsWith("image/") || file.mime_type.startsWith("text/") || file.mime_type === "application/json"
  );
  return (
    <div className={cx(
      "flex min-w-0 flex-col gap-3 rounded-lg border bg-white p-3 shadow-sm transition hover:border-slate-300 hover:shadow-md sm:flex-row sm:items-center",
      file.conflict_of ? "border-amber-300" : "border-slate-200"
    )}>
      <div className="flex min-w-0 flex-1 items-center gap-3">
        <div className={cx(
          "flex h-9 w-9 shrink-0 items-center justify-center rounded-lg text-xs font-bold",
          file.is_dir ? "bg-blue-50 text-blue-700" : "bg-slate-100 text-slate-600"
        )}>
          {file.is_dir ? "DIR" : "FILE"}
        </div>
        <div className="min-w-0">
          <p className="truncate text-sm font-semibold text-slate-950">{file.name}</p>
          <p className="truncate text-xs text-slate-500">
            {file.is_dir ? "Directory" : formatBytes(file.size_bytes)}
            {file.modified_at && ` · Modified ${new Date(file.modified_at).toLocaleDateString()}`}
          </p>
        </div>
      </div>
      <div className="min-w-0">
        {file.conflict_of && (
          <p className="mb-2 truncate rounded-md bg-amber-50 px-2 py-1 text-xs text-amber-700">Conflict with {file.conflict_of}</p>
        )}
      </div>
      <div className="grid grid-cols-2 gap-2 sm:flex sm:shrink-0">
        {canPreview && (
          <button
            onClick={() => onPreview(file)}
            className="rounded-lg bg-green-50 px-3 py-2 text-xs font-semibold text-green-700 hover:bg-green-100 active:bg-green-200"
          >
            Preview
          </button>
        )}
        {!file.is_dir && (
          <button
            onClick={() => onDownload(file)}
            className="rounded-lg bg-blue-50 px-3 py-2 text-xs font-semibold text-blue-700 hover:bg-blue-100 active:bg-blue-200"
          >
            Download
          </button>
        )}
        <button
          onClick={() => onRename(file)}
          className="rounded-lg bg-slate-100 px-3 py-2 text-xs font-semibold text-slate-700 hover:bg-slate-200 active:bg-slate-300"
        >
          Rename
        </button>
        <button
          onClick={() => onDelete(file)}
          className="rounded-lg bg-red-50 px-3 py-2 text-xs font-semibold text-red-700 hover:bg-red-100 active:bg-red-200"
        >
          Delete
        </button>
      </div>
    </div>
  );
}
function FilePreview({ file, onClose }) {
  const [content, setContent] = useState(null);
  const [loading, setLoading] = useState(true);
  const url = `${API}/files/download?path=${encodeURIComponent(file.path)}`;
  useEffect(() => {
    if (file.mime_type?.startsWith("image/")) {
      setLoading(false);
      return;
    }
    fetch(url, { credentials: "same-origin" })
      .then(r => r.text())
      .then(t => { setContent(t); setLoading(false); })
      .catch(() => { setContent("Failed to load"); setLoading(false); });
  }, [file.path]);
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-950/55 p-4" onClick={onClose}>
      <div className="flex max-h-[90vh] w-full max-w-4xl flex-col overflow-hidden rounded-lg border border-slate-200 bg-white shadow-2xl" onClick={e => e.stopPropagation()}>
        <div className="flex items-center justify-between border-b border-slate-200 px-4 py-3">
          <div className="min-w-0">
            <p className="truncate text-sm font-semibold text-slate-950">{file.name}</p>
            <p className="text-xs text-slate-500">{formatBytes(file.size_bytes)} · {file.mime_type}</p>
          </div>
          <div className="ml-4 flex shrink-0 gap-2">
            <a href={url} download className="rounded-lg bg-blue-50 px-3 py-2 text-xs font-semibold text-blue-700 hover:bg-blue-100">Download</a>
            <button onClick={onClose} className="rounded-lg bg-slate-100 px-3 py-2 text-xs font-semibold text-slate-700 hover:bg-slate-200">Close</button>
          </div>
        </div>
        <div className="flex-1 overflow-auto bg-slate-50 p-4">
          {loading ? (
            <div className="py-8 text-center text-sm text-slate-400">Loading preview...</div>
          ) : file.mime_type?.startsWith("image/") ? (
            <img src={url} alt={file.name} className="max-w-full max-h-[70vh] mx-auto object-contain" />
          ) : (
            <pre className="rounded-lg border border-slate-200 bg-white p-4 font-mono text-sm whitespace-pre-wrap break-words text-slate-700">{content}</pre>
          )}
        </div>
      </div>
    </div>
  );
}

function RenameDialog({ file, busy, error, onSubmit, onCancel }) {
  const [newPath, setNewPath] = useState(file.path);
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-950/45 p-4" onMouseDown={onCancel}>
      <form
        onSubmit={(e) => { e.preventDefault(); onSubmit(newPath); }}
        className="w-full max-w-md rounded-lg border border-slate-200 bg-white p-5 shadow-2xl"
        onMouseDown={(e) => e.stopPropagation()}
      >
        <h3 className="text-base font-semibold text-slate-950">Rename item</h3>
        <p className="mt-1 truncate text-sm text-slate-500">{file.name}</p>
        <label className="mt-4 block">
          <span className="mb-1 block text-xs font-semibold text-slate-500">New pool path</span>
          <input
            autoFocus
            type="text"
            value={newPath}
            onChange={(e) => setNewPath(e.target.value)}
            className="w-full rounded-lg border border-slate-300 px-3 py-2 text-sm outline-none focus:border-blue-500 focus:ring-2 focus:ring-blue-100"
          />
        </label>
        {error && <div className="mt-3"><InlineMessage tone="error">{error}</InlineMessage></div>}
        <div className="mt-5 flex justify-end gap-2">
          <button type="button" onClick={onCancel} className="rounded-lg px-4 py-2 text-sm font-medium text-slate-600 hover:bg-slate-100">Cancel</button>
          <button type="submit" disabled={busy || !newPath || newPath === file.path} className="rounded-lg bg-blue-600 px-4 py-2 text-sm font-semibold text-white hover:bg-blue-700 disabled:opacity-50">
            {busy ? "Renaming..." : "Rename"}
          </button>
        </div>
      </form>
    </div>
  );
}

function FilesPage() {
  const [path, setPath] = useState("/");
  const [files, setFiles] = useState([]);
  const [search, setSearch] = useState("");
  const [uploading, setUploading] = useState(false);
  const [uploadProgress, setUploadProgress] = useState(null);
  const [refreshing, setRefreshing] = useState(false);
  const [previewFile, setPreviewFile] = useState(null);
  const [loading, setLoading] = useState(true);
  const [message, setMessage] = useState(null);
  const [renameFile, setRenameFile] = useState(null);
  const [deleteFile, setDeleteFile] = useState(null);
  const [actionBusy, setActionBusy] = useState(false);
  const [actionError, setActionError] = useState(null);
  const loadFiles = useCallback(async () => {
    setLoading(true);
    const q = search ? `?q=${encodeURIComponent(search)}` : `?path=${encodeURIComponent(path)}`;
    try {
      const res = await api(`/files${q}`);
      if (res.ok) {
        setFiles(res.data.entries || []);
      } else {
        setMessage({ tone: "error", text: res.error?.message || "Failed to load files" });
      }
    } catch (err) {
      setMessage({ tone: "error", text: err.message || "Failed to load files" });
    } finally {
      setLoading(false);
    }
  }, [path, search]);
  useEffect(() => { loadFiles(); }, [loadFiles]);
  const handleRefresh = async () => {
    setRefreshing(true);
    await loadFiles();
    setRefreshing(false);
  };
  const handleUpload = (e) => {
    const file = e.target.files[0];
    if (!file) return;
    setMessage(null);
    setUploading(true);
    setUploadProgress(0);
    const formData = new FormData();
    formData.append("path", path === "/" ? `/${file.name}` : `${path}/${file.name}`);
    formData.append("file", file);
    const xhr = new XMLHttpRequest();
    xhr.upload.onprogress = (ev) => {
      if (ev.lengthComputable) setUploadProgress(Math.round(ev.loaded / ev.total * 100));
    };
    xhr.onload = () => {
      setUploading(false);
      setUploadProgress(null);
      try {
        const data = JSON.parse(xhr.responseText);
        if (!data.ok) setMessage({ tone: "error", text: `Upload failed: ${data.error?.message || "Unknown error"}` });
        else setMessage({ tone: "success", text: `Uploaded ${file.name}` });
      } catch { /* ignore */ }
      loadFiles();
    };
    xhr.onerror = () => {
      setUploading(false);
      setUploadProgress(null);
      setMessage({ tone: "error", text: "Upload failed. Check the agent connection and try again." });
    };
    xhr.open("POST", `${API}/files/upload`);
    xhr.withCredentials = true;
    xhr.send(formData);
    e.target.value = "";
  };
  const handleDownload = (file) => {
    window.location.assign(`${API}/files/download?path=${encodeURIComponent(file.path)}`);
  };
  const handleDelete = async () => {
    if (!deleteFile) return;
    setActionBusy(true);
    setActionError(null);
    try {
      const res = await fetch(`${API}/files?path=${encodeURIComponent(deleteFile.path)}`, { method: "DELETE", credentials: "same-origin" });
      if (!res.ok) {
        const err = await res.json().catch(() => ({}));
        throw new Error(err.error?.message || res.statusText);
      }
      setMessage({ tone: "success", text: `Deleted ${deleteFile.name}` });
      setDeleteFile(null);
      loadFiles();
    } catch (err) {
      setActionError(err.message || "Delete failed");
    } finally {
      setActionBusy(false);
    }
  };
  const handleRename = async (newPath) => {
    if (!renameFile) return;
    if (!newPath || newPath === renameFile.path) return;
    setActionBusy(true);
    setActionError(null);
    try {
      const res = await fetch(`${API}/files/rename`, {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        credentials: "same-origin",
        body: JSON.stringify({ path: renameFile.path, new_path: newPath }),
      });
      if (!res.ok) {
        const err = await res.json().catch(() => ({}));
        throw new Error(err.error?.message || res.statusText);
      }
      setMessage({ tone: "success", text: `Renamed ${renameFile.name}` });
      setRenameFile(null);
      loadFiles();
    } catch (err) {
      setActionError(err.message || "Rename failed");
    } finally {
      setActionBusy(false);
    }
  };
  const totalSize = files.reduce((sum, f) => sum + (f.is_dir ? 0 : (f.size_bytes || 0)), 0);
  const folders = files.filter((f) => f.is_dir).length;
  return (
    <div className="space-y-4">
      <PageHeader
        eyebrow="Storage pool"
        title="Files"
        description="Upload, preview, rename, and download files stored across the pool."
        action={
          <label className="block">
            <span className="inline-flex cursor-pointer items-center rounded-lg bg-blue-600 px-4 py-2.5 text-sm font-semibold text-white shadow-sm hover:bg-blue-700 active:bg-blue-800">
              {uploading ? `Uploading ${uploadProgress ?? 0}%` : "Upload file"}
            </span>
            <input type="file" className="hidden" onChange={handleUpload} disabled={uploading} />
          </label>
        }
      />
      <div className="grid gap-3 sm:grid-cols-3">
        <div className="rounded-lg border border-slate-200 bg-white p-4 shadow-sm">
          <p className="text-xs font-semibold uppercase text-slate-500">Visible items</p>
          <p className="mt-1 text-2xl font-semibold text-slate-950">{files.length}</p>
        </div>
        <div className="rounded-lg border border-slate-200 bg-white p-4 shadow-sm">
          <p className="text-xs font-semibold uppercase text-slate-500">Folders</p>
          <p className="mt-1 text-2xl font-semibold text-slate-950">{folders}</p>
        </div>
        <div className="rounded-lg border border-slate-200 bg-white p-4 shadow-sm">
          <p className="text-xs font-semibold uppercase text-slate-500">Visible file size</p>
          <p className="mt-1 text-2xl font-semibold text-slate-950">{formatBytes(totalSize)}</p>
        </div>
      </div>
      <Section>
        <div className="flex flex-col gap-3 lg:flex-row">
          <div className="flex flex-1 items-center rounded-lg border border-slate-300 bg-white px-3 focus-within:border-blue-500 focus-within:ring-2 focus-within:ring-blue-100">
            <span className="text-sm text-slate-400">Search</span>
            <input
              type="text"
              placeholder="name, type, or path"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              className="min-w-0 flex-1 border-0 bg-transparent px-3 py-3 text-sm outline-none"
            />
            {search && (
              <button onClick={() => setSearch("")} className="rounded-md px-2 py-1 text-xs font-semibold text-slate-500 hover:bg-slate-100">
                Clear
              </button>
            )}
          </div>
          <button
            onClick={handleRefresh}
            disabled={refreshing}
            className="rounded-lg bg-slate-100 px-4 py-3 text-sm font-semibold text-slate-700 hover:bg-slate-200 active:bg-slate-300 disabled:opacity-50"
          >
            {refreshing ? "Refreshing..." : "Refresh"}
          </button>
        </div>
        {uploading && uploadProgress !== null && (
          <div className="mt-4 space-y-2">
            <ProgressBar value={uploadProgress} />
            <p className="text-xs font-medium text-slate-500">Uploading into {path}</p>
          </div>
        )}
      </Section>
      {message && <InlineMessage tone={message.tone}>{message.text}</InlineMessage>}
      <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
        {loading ? (
          <EmptyState title="Loading files..." description="Checking the current pool path." />
        ) : files.length > 0 ? (
          files.map((f) => (
            <FileCard
              key={f.file_id || f.path}
              file={f}
              onDownload={handleDownload}
              onDelete={(file) => { setActionError(null); setDeleteFile(file); }}
              onRename={(file) => { setActionError(null); setRenameFile(file); }}
              onPreview={setPreviewFile}
            />
          ))
        ) : (
          <EmptyState
            title={search ? "No matching files" : "No files yet"}
            description={search ? "Try a shorter search term or clear the search." : "Upload a file to start filling this pool."}
          />
        )}
      </div>
      {previewFile && <FilePreview file={previewFile} onClose={() => setPreviewFile(null)} />}
      {renameFile && (
        <RenameDialog
          file={renameFile}
          busy={actionBusy}
          error={actionError}
          onCancel={() => { setRenameFile(null); setActionError(null); }}
          onSubmit={handleRename}
        />
      )}
      {deleteFile && (
        <ConfirmDialog
          title="Delete file?"
          message={`This removes "${deleteFile.name}" from the pool. This action cannot be undone.`}
          confirmLabel="Delete"
          busy={actionBusy}
          onCancel={() => { setDeleteFile(null); setActionError(null); }}
          onConfirm={handleDelete}
        />
      )}
      {deleteFile && actionError && (
        <div className="fixed bottom-24 left-4 right-4 z-50 mx-auto max-w-md lg:bottom-6">
          <InlineMessage tone="error">{actionError}</InlineMessage>
        </div>
      )}
    </div>
  );
}

function NodesPage() {
  const [nodes, setNodes] = useState([]);
  const [pendingJoins, setPendingJoins] = useState([]);
  const [invite, setInvite] = useState(null);
  const [inviteCopied, setInviteCopied] = useState(false);
  const [creatingInvite, setCreatingInvite] = useState(false);
  const [showSwitch, setShowSwitch] = useState(false);
  const [switchAddr, setSwitchAddr] = useState("");
  const [switchUser, setSwitchUser] = useState("");
  const [switchPass, setSwitchPass] = useState("");
  const [switchToken, setSwitchToken] = useState("");
  const [switching, setSwitching] = useState(false);
  const [switchError, setSwitchError] = useState(null);
  const [scanning, setScanning] = useState(false);
  const [scanMessage, setScanMessage] = useState(null);

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
  const usedPct = totalBytes > 0 ? Math.round((usedBytes / totalBytes) * 100) : 0;

  const createInvite = async () => {
    setCreatingInvite(true);
    setInviteCopied(false);
    try {
      const res = await api("/invites", { method: "POST" });
      if (res.ok) setInvite(res.data);
    } finally {
      setCreatingInvite(false);
    }
  };

  const copyInvite = async () => {
    if (!invite?.join_token) return;
    try {
      await navigator.clipboard.writeText(invite.join_token);
      setInviteCopied(true);
      setTimeout(() => setInviteCopied(false), 1800);
    } catch {
      setInviteCopied(false);
    }
  };

  return (
    <div className="space-y-4">
      <PageHeader
        eyebrow="Cluster"
        title="Nodes"
        description="Monitor capacity, approve new devices, and connect this agent to another pool."
        action={
          <button
            onClick={createInvite}
            disabled={creatingInvite}
            className="rounded-lg bg-blue-600 px-4 py-2.5 text-sm font-semibold text-white shadow-sm hover:bg-blue-700 disabled:opacity-50"
          >
            {creatingInvite ? "Creating..." : "Create invite"}
          </button>
        }
      />

      <div className="grid gap-3 md:grid-cols-4">
        <div className="rounded-lg border border-slate-200 bg-white p-4 shadow-sm">
          <p className="text-xs font-semibold uppercase text-slate-500">Nodes</p>
          <p className="mt-1 text-2xl font-semibold text-slate-950">{nodes.length}</p>
        </div>
        <div className="rounded-lg border border-slate-200 bg-white p-4 shadow-sm">
          <p className="text-xs font-semibold uppercase text-slate-500">Online</p>
          <p className="mt-1 text-2xl font-semibold text-green-700">{onlineCount}</p>
        </div>
        <div className="rounded-lg border border-slate-200 bg-white p-4 shadow-sm">
          <p className="text-xs font-semibold uppercase text-slate-500">Used</p>
          <p className="mt-1 text-2xl font-semibold text-slate-950">{formatBytes(usedBytes)}</p>
        </div>
        <div className="rounded-lg border border-slate-200 bg-white p-4 shadow-sm">
          <p className="text-xs font-semibold uppercase text-slate-500">Capacity</p>
          <p className="mt-1 text-2xl font-semibold text-slate-950">{formatBytes(totalBytes)}</p>
        </div>
      </div>
      <Section title="Pool capacity" description={`${usedPct}% used across online and known nodes`}>
        <ProgressBar value={usedPct} tone={usedPct > 80 ? "amber" : "blue"} />
        <div className="mt-2 flex justify-between text-xs text-slate-500">
          <span>{formatBytes(usedBytes)} used</span>
          <span>{formatBytes(Math.max(0, totalBytes - usedBytes))} free</span>
        </div>
      </Section>

      {pendingJoins.length > 0 && (
        <Section title="Pending join requests" description="Review new devices before they join this pool." className="border-amber-200">
          <div className="space-y-2">
            {pendingJoins.map((pj) => (
              <div key={pj.node_id} className="flex flex-col gap-3 rounded-lg bg-amber-50 p-3 sm:flex-row sm:items-center sm:justify-between">
                <div className="min-w-0">
                  <p className="truncate text-sm font-semibold text-slate-950">{pj.name || pj.node_id.slice(0, 8)}</p>
                  <p className="truncate text-xs text-slate-500">{pj.platform} / {pj.address}</p>
                </div>
                <button
                  onClick={() => approveJoin(pj.node_id)}
                  className="rounded-lg bg-green-600 px-3 py-2 text-xs font-semibold text-white hover:bg-green-700"
                >
                  Approve
                </button>
              </div>
            ))}
          </div>
        </Section>
      )}

      <Section title="Invite token" description="Create a one-time token for a nearby device. Tokens expire in 15 minutes.">
        {invite && (
          <div className="rounded-lg bg-slate-50 p-3">
            <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
              <div className="min-w-0">
                <p className="mb-1 text-xs font-semibold uppercase text-slate-500">Join token</p>
                <code className="block break-all font-mono text-sm text-slate-800">{invite.join_token}</code>
                <p className="mt-2 text-xs text-slate-400">Expires {new Date(invite.expires_at).toLocaleString()}</p>
              </div>
              <button onClick={copyInvite} className="rounded-lg bg-white px-3 py-2 text-xs font-semibold text-slate-700 shadow-sm ring-1 ring-slate-200 hover:bg-slate-100">
                {inviteCopied ? "Copied" : "Copy"}
              </button>
            </div>
          </div>
        )}
        {!invite && <EmptyState title="No active invite" description="Create an invite when you are ready to add another device." />}
      </Section>

      <Section
        title="Find nearby pools"
        description="Scan the local network, then use the discovered address to join another pool."
        action={
          <button
            onClick={async () => {
              setScanning(true);
              setScanMessage(null);
              try {
                const r = await api("/network/scan");
                if (r.ok && r.data?.nodes?.length > 0) {
                  const firstNode = r.data.nodes[0];
                  setSwitchAddr(`http://${firstNode.address}`);
                  setShowSwitch(true);
                  setScanMessage({ tone: "success", text: `Found ${r.data.nodes.length} node(s). Filled the first address below.` });
                } else {
                  setScanMessage({ tone: "warning", text: "No PocketCluster nodes were found on this network." });
                }
              } catch (e) {
                setScanMessage({ tone: "error", text: e.message || "Scan failed" });
              } finally {
                setScanning(false);
              }
            }}
            disabled={scanning}
            className="rounded-lg bg-slate-100 px-4 py-2 text-sm font-semibold text-slate-700 hover:bg-slate-200 disabled:opacity-50"
          >
            {scanning ? "Scanning..." : "Scan network"}
          </button>
        }
      >
        {scanMessage ? <InlineMessage tone={scanMessage.tone}>{scanMessage.text}</InlineMessage> : <p className="text-sm text-slate-500">Scan uses local discovery and may take a moment on busy Wi-Fi networks.</p>}
      </Section>

      <Section title="Join another pool">
        <button
          onClick={() => setShowSwitch(!showSwitch)}
          className="w-full rounded-lg bg-slate-100 px-3 py-2 text-left text-sm font-semibold text-slate-700 hover:bg-slate-200"
        >
          {showSwitch ? "Hide join form" : "Show join form"}
        </button>
        {showSwitch && (
          <form onSubmit={handleSwitch} className="mt-4 grid gap-3 md:grid-cols-2">
            <input
              type="text"
              value={switchAddr}
              onChange={(e) => setSwitchAddr(e.target.value)}
              placeholder="Pool address (e.g. http://192.168.1.10:7788)"
              required
              className="w-full rounded-lg border border-slate-300 px-4 py-3 text-sm outline-none focus:border-blue-500 focus:ring-2 focus:ring-blue-100 md:col-span-2"
            />
            <input
              type="text"
              value={switchUser}
              onChange={(e) => setSwitchUser(e.target.value)}
              placeholder="Pool username"
              required
              className="w-full rounded-lg border border-slate-300 px-4 py-3 text-sm outline-none focus:border-blue-500 focus:ring-2 focus:ring-blue-100"
            />
            <input
              type="password"
              value={switchPass}
              onChange={(e) => setSwitchPass(e.target.value)}
              placeholder="Pool password"
              required
              className="w-full rounded-lg border border-slate-300 px-4 py-3 text-sm outline-none focus:border-blue-500 focus:ring-2 focus:ring-blue-100"
            />
            <input
              type="text"
              value={switchToken}
              onChange={(e) => setSwitchToken(e.target.value)}
              placeholder="Invite token (optional)"
              className="w-full rounded-lg border border-slate-300 px-4 py-3 text-sm outline-none focus:border-blue-500 focus:ring-2 focus:ring-blue-100 md:col-span-2"
            />
            {switchError && <div className="md:col-span-2"><InlineMessage tone="error">{switchError}</InlineMessage></div>}
            <button
              type="submit"
              disabled={switching || !switchAddr}
              className="rounded-lg bg-blue-600 py-3 text-sm font-semibold text-white hover:bg-blue-700 disabled:opacity-50 md:col-span-2"
            >
              {switching ? "Joining…" : "Switch pool"}
            </button>
          </form>
        )}
      </Section>

      <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
        {nodes.length > 0 ? nodes.map((n) => <NodeCard key={n.node_id} node={n} />) : (
          <EmptyState title="No nodes yet" description="Create a pool or join an existing pool to see devices here." />
        )}
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
    <div className="space-y-4">
      <PageHeader
        eyebrow="Diagnostics"
        title="Logs"
        description="Switch between raw agent output and pool events without losing your place."
        action={
          <button
            onClick={handleRefresh}
            disabled={refreshing}
            className="rounded-lg bg-slate-100 px-4 py-2.5 text-sm font-semibold text-slate-700 hover:bg-slate-200 disabled:opacity-50"
          >
            {refreshing ? "Refreshing..." : "Refresh"}
          </button>
        }
      />

      <div className="inline-flex w-full rounded-lg bg-slate-200 p-1 sm:w-auto">
        {[
          ["agent", "Agent logs"],
          ["events", "Events"],
        ].map(([id, label]) => (
          <button
            key={id}
            onClick={() => setView(id)}
            className={cx(
              "flex-1 rounded-md px-4 py-2 text-sm font-semibold sm:flex-none",
              view === id ? "bg-white text-slate-950 shadow-sm" : "text-slate-600 hover:text-slate-950"
            )}
          >
            {label}
          </button>
        ))}
      </div>

      {view === "agent" && (
        <div className="max-h-[65vh] overflow-y-auto rounded-lg border border-slate-800 bg-slate-950 p-3 shadow-sm">
          {agentLogs.length === 0 ? (
            <p className="py-8 text-center text-xs text-slate-400">No agent logs yet</p>
          ) : (
            agentLogs.map((line, i) => (
              <p key={i} className="font-mono text-xs leading-relaxed whitespace-pre-wrap break-all text-emerald-300">
                {line}
              </p>
            ))
          )}
        </div>
      )}

      {view === "events" && (
        <div className="space-y-2">
          {logs.map((log, i) => (
            <div key={i} className="rounded-lg border border-slate-200 bg-white p-3 shadow-sm">
              <div className="mb-1 flex items-center justify-between gap-3">
                <span className={`text-xs font-semibold ${typeColor[log.type] || "text-slate-600"}`}>
                  {log.type}
                </span>
                <span className="shrink-0 text-xs text-slate-400">{log.timestamp}</span>
              </div>
              <p className="truncate font-mono text-xs text-slate-500">Node: {log.node_id?.slice(0, 8)}...</p>
            </div>
          ))}
          {logs.length === 0 && (
            <EmptyState title="No events yet" description="Pool activity will appear here after file and node changes." />
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
  const [joinUser, setJoinUser] = useState("");
  const [joinPass, setJoinPass] = useState("");
  const [createUser, setCreateUser] = useState("");
  const [createPass, setCreatePass] = useState("");
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
    if (!createUser || !createPass) { setError("Username and password are required"); return; }
    setLoading(true);
    setError(null);
    try {
      const r = await api("/cluster", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ username: createUser, password: createPass }),
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
    if (!joinUser || !joinPass) { setError("Pool username and password are required"); return; }
    setLoading(true);
    setError(null);
    try {
      const addr = selectedAddr || bootstrap;
      const r = await api("/join", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ bootstrap: addr, join_token: token, pool_user: joinUser, pool_password: joinPass }),
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
            value={joinUser}
            onChange={(e) => setJoinUser(e.target.value)}
            placeholder="Pool username"
            required
            className="w-full border rounded-lg px-4 py-3 text-sm"
          />
          <input
            type="password"
            value={joinPass}
            onChange={(e) => setJoinPass(e.target.value)}
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
                value={createUser}
                onChange={(e) => setCreateUser(e.target.value)}
                placeholder="Pool username"
                required
                className="w-full border rounded-lg px-4 py-3 text-sm"
              />
              <input
                type="password"
                value={createPass}
                onChange={(e) => setCreatePass(e.target.value)}
                placeholder="Pool password"
                required
                className="w-full border rounded-lg px-4 py-3 text-sm"
              />
              {error && <p className="text-sm text-red-600">{error}</p>}
              <button
                type="submit"
                disabled={loading || !createUser || !createPass}
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
  // Auto-refresh every 10 seconds
  useEffect(() => {
    const id = setInterval(load, 10_000);
    return () => clearInterval(id);
  }, [load]);
  if (loading) return <div className="py-16 text-center text-sm text-slate-400">Loading health data...</div>;
  if (!summary) return <div className="py-16 text-center text-sm text-slate-400">Health data unavailable</div>;
  const statusColor = {
    healthy: "border-green-200 bg-green-50 text-green-700",
    under_replicated: "border-amber-200 bg-amber-50 text-amber-700",
    unavailable: "border-red-200 bg-red-50 text-red-700",
    repairing: "border-blue-200 bg-blue-50 text-blue-700",
  };
  return (
    <div className="space-y-5">
      <PageHeader
        eyebrow="Replication"
        title="Health"
        description="Track chunk availability, replica coverage, and repair activity across the pool."
      />
      <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
        <div className={`rounded-lg border p-4 shadow-sm ${statusColor[summary.overall_status] || "border-slate-200 bg-white text-slate-700"}`}>
          <div className="text-xs font-semibold uppercase opacity-70">Overall</div>
          <div className="mt-1 text-lg font-bold capitalize">{summary.overall_status}</div>
        </div>
        <div className="rounded-lg border border-slate-200 bg-white p-4 shadow-sm">
          <div className="text-xs font-semibold uppercase text-slate-500">Files</div>
          <div className="mt-1 text-lg font-bold text-slate-950">{summary.total_files}</div>
        </div>
        <div className="rounded-lg border border-slate-200 bg-white p-4 shadow-sm">
          <div className="text-xs font-semibold uppercase text-slate-500">Chunks</div>
          <div className="mt-1 text-lg font-bold text-slate-950">{summary.total_chunks}</div>
        </div>
        <div className="rounded-lg border border-slate-200 bg-white p-4 shadow-sm">
          <div className="text-xs font-semibold uppercase text-slate-500">Healthy</div>
          <div className="mt-1 text-lg font-bold text-green-700">{summary.healthy_chunks}</div>
        </div>
      </div>
      <div className="grid grid-cols-3 gap-3">
        <div className="rounded-lg border border-slate-200 bg-white p-4 shadow-sm">
          <div className="text-xs font-semibold uppercase text-slate-500">Under-replicated</div>
          <div className={`mt-1 text-lg font-bold ${summary.under_replicated_chunks > 0 ? "text-amber-600" : "text-slate-400"}`}>
            {summary.under_replicated_chunks}
          </div>
        </div>
        <div className="rounded-lg border border-slate-200 bg-white p-4 shadow-sm">
          <div className="text-xs font-semibold uppercase text-slate-500">Unavailable</div>
          <div className={`mt-1 text-lg font-bold ${summary.unavailable_chunks > 0 ? "text-red-600" : "text-slate-400"}`}>
            {summary.unavailable_chunks}
          </div>
        </div>
        <div className="rounded-lg border border-slate-200 bg-white p-4 shadow-sm">
          <div className="text-xs font-semibold uppercase text-slate-500">Repairing</div>
          <div className={`mt-1 text-lg font-bold ${summary.repairing_chunks > 0 ? "text-blue-600" : "text-slate-400"}`}>
            {summary.repairing_chunks}
          </div>
        </div>
      </div>
      <div className="rounded-lg border border-slate-200 bg-white px-4 py-3 text-xs text-slate-500 shadow-sm">
        Last scan: <span className="font-medium text-slate-700">{summary.last_scan_at ? new Date(summary.last_scan_at).toLocaleString() : "never"}</span>
        {summary.last_repair_at && <> · Last repair: <span className="font-medium text-slate-700">{new Date(summary.last_repair_at).toLocaleString()}</span></>}
      </div>
      {selectedChunk && (
        <div className="rounded-lg border border-blue-200 bg-white p-4 shadow-sm">
          <div className="mb-3 flex items-center justify-between">
            <h3 className="text-sm font-semibold text-slate-950">Chunk detail</h3>
            <button onClick={() => setSelectedChunk(null)} className="rounded-md px-2 py-1 text-sm text-slate-400 hover:bg-slate-100 hover:text-slate-700">Close</button>
          </div>
          <div className="mb-2 break-all font-mono text-xs text-slate-500">{selectedChunk.chunk_id}</div>
          <div className="mb-3 grid grid-cols-2 gap-2 text-sm text-slate-700">
            <div>Size: {formatBytes(selectedChunk.size_bytes)}</div>
            <div>Replicas: {selectedChunk.online_replicas}/{selectedChunk.target_replicas}</div>
            <div>Status: <span className={`font-medium ${selectedChunk.status === "healthy" ? "text-green-600" : selectedChunk.status === "unavailable" ? "text-red-600" : "text-yellow-600"}`}>{selectedChunk.status}</span></div>
          </div>
          {selectedChunk.replica_nodes && selectedChunk.replica_nodes.length > 0 && (
            <div>
              <div className="mb-1 text-xs font-semibold text-slate-500">Replica Nodes</div>
              <div className="space-y-1">
                {selectedChunk.replica_nodes.map((r) => (
                  <div key={r.node_id} className="flex items-center gap-2 text-xs">
                    <span className={`w-2 h-2 rounded-full ${r.online ? "bg-green-500" : "bg-gray-300"}`}></span>
                    <span className="font-mono">{r.node_id}</span>
                    <span className="text-slate-400">{r.online ? "online" : "offline"}</span>
                  </div>
                ))}
              </div>
            </div>
          )}
          {selectedChunk.referencing_files && selectedChunk.referencing_files.length > 0 && (
            <div className="mt-3">
              <div className="mb-1 text-xs font-semibold text-slate-500">Referencing Files</div>
              <div className="space-y-1">
                {selectedChunk.referencing_files.map((p) => (
                  <div key={p} className="font-mono text-xs text-slate-600">{p}</div>
                ))}
              </div>
            </div>
          )}
        </div>
      )}
      <div>
        <h3 className="mb-3 text-sm font-semibold text-slate-950">All Chunks ({chunks.length})</h3>
        <div className="space-y-2">
          {chunks.map((c) => (
            <button
              key={c.chunk_id}
              onClick={() => setSelectedChunk(c)}
              className={cx(
                "w-full rounded-lg border border-slate-200 bg-white p-3 text-left shadow-sm transition hover:border-slate-300 hover:shadow-md",
                selectedChunk?.chunk_id === c.chunk_id && "ring-2 ring-blue-500"
              )}
            >
              <div className="flex items-center justify-between">
                <div className="max-w-[220px] truncate font-mono text-xs text-slate-500">{c.chunk_id}</div>
                <span className={`inline-block px-2 py-0.5 rounded text-xs font-medium ${
                  c.status === "healthy" ? "bg-green-100 text-green-700" :
                  c.status === "unavailable" ? "bg-red-100 text-red-700" :
                  c.status === "repairing" ? "bg-blue-100 text-blue-700" :
                  "bg-yellow-100 text-yellow-700"
                }`}>
                  {c.status}
                </span>
              </div>
              <div className="mt-1 flex gap-4 text-xs text-slate-400">
                <span>{formatBytes(c.size_bytes)}</span>
                <span>{c.online_replicas}/{c.target_replicas} replicas</span>
                {c.referencing_files && <span>{c.referencing_files.length} file(s)</span>}
              </div>
            </button>
          ))}
          {chunks.length === 0 && (
            <EmptyState title="No chunks found" description="Health data will populate after files are added to the pool." />
          )}
        </div>
      </div>
    </div>
  );
}
const navItems = [
  { id: "files", label: "Files", hint: "Pool browser" },
  { id: "nodes", label: "Nodes", hint: "Devices" },
  { id: "health", label: "Health", hint: "Replication" },
  { id: "logs", label: "Logs", hint: "Events" },
];


export default function App() {
  const [tab, setTab] = useState("files");
  const [clusterId, setClusterId] = useState(null);
  const [discoveryMode, setDiscoveryMode] = useState("auto");
  const [loading, setLoading] = useState(true);
  const [needsLogin, setNeedsLogin] = useState(false);
  const [noCluster, setNoCluster] = useState(false);
  const [startupError, setStartupError] = useState(null);

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
    }).catch((err) => {
      setStartupError(err.message || "Unable to reach the local agent");
      setLoading(false);
    });
  }, []);

  if (loading) return <div className="flex min-h-screen items-center justify-center text-sm text-slate-400">Loading...</div>;
  if (startupError) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-slate-50 p-4">
        <div className="w-full max-w-md rounded-lg border border-red-200 bg-white p-5 shadow-sm">
          <h1 className="text-lg font-semibold text-slate-950">PocketCluster cannot reach the local agent</h1>
          <p className="mt-2 text-sm leading-6 text-slate-600">{startupError}</p>
        </div>
      </div>
    );
  }
  if (needsLogin) return <LoginPage />;
  if (noCluster || !clusterId) return <JoinPage mode={discoveryMode} />;

  return (
    <div className="min-h-screen bg-slate-50 lg:flex" style={{ paddingTop: 'env(safe-area-inset-top)' }}>
      <header className="border-b border-slate-200 bg-white/95 px-4 py-3 backdrop-blur lg:fixed lg:inset-y-0 lg:left-0 lg:w-64 lg:border-b-0 lg:border-r lg:px-5 lg:py-6">
        <div className="flex items-center justify-between gap-3 lg:block">
          <div>
            <h1 className="text-lg font-bold text-slate-950 lg:text-2xl">PocketCluster</h1>
            <p className="hidden text-xs text-slate-500 lg:mt-1 lg:block">LAN storage pool</p>
          </div>
          <div className="min-w-0 rounded-lg bg-slate-100 px-3 py-2 text-right lg:mt-5 lg:text-left">
            <p className="text-[10px] font-semibold uppercase text-slate-500">Cluster</p>
            <p className="max-w-[160px] truncate font-mono text-xs text-slate-700">{clusterId || "unknown"}</p>
          </div>
        </div>
        <nav className="mt-8 hidden lg:block">
          <div className="space-y-1">
            {navItems.map((item) => (
              <button
                key={item.id}
                onClick={() => setTab(item.id)}
                className={cx(
                  "flex w-full items-center justify-between rounded-lg px-3 py-2.5 text-left transition",
                  tab === item.id ? "bg-blue-50 text-blue-700" : "text-slate-600 hover:bg-slate-100 hover:text-slate-950"
                )}
              >
                <span className="text-sm font-semibold">{item.label}</span>
                <span className="text-xs opacity-70">{item.hint}</span>
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
        className="fixed bottom-0 left-0 right-0 z-50 border-t border-slate-200 bg-white/95 shadow-[0_-8px_24px_rgba(15,23,42,0.08)] backdrop-blur lg:hidden"
        style={{ paddingBottom: 'max(0.5rem, env(safe-area-inset-bottom))' }}
      >
        <div className="grid grid-cols-4">
          {navItems.map((item) => (
            <button
              key={item.id}
              onClick={() => setTab(item.id)}
              className={cx(
                "min-w-0 px-1 py-3 text-center",
                tab === item.id ? "text-blue-700" : "text-slate-500"
              )}
            >
              <div className="truncate text-xs font-semibold">{item.label}</div>
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
