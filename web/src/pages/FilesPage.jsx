import { useCallback, useEffect, useState } from "react";
import { API, api } from "../api";
import { cx, formatBytes } from "../utils";
import { ConfirmDialog, EmptyState, InlineMessage, PageHeader, ProgressBar, Section } from "../components/common";

function ReplicaReadiness({ status }) {
  const states = {
    healthy: { label: "副本已就绪", className: "bg-emerald-50 text-emerald-700" },
    under_replicated: { label: "正在复制", className: "bg-amber-50 text-amber-700" },
    repairing: { label: "正在修复", className: "bg-blue-50 text-blue-700" },
    unavailable: { label: "暂不可用", className: "bg-red-50 text-red-700" },
  };
  const state = states[status];
  if (!state) return null;
  return <span className={`rounded-full px-2 py-1 text-[11px] font-semibold ${state.className}`}>{state.label}</span>;
}

function FileCard({ file, onDownload, onDelete, onRename, onPreview, onHistory }) {
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
          {file.is_dir ? "目录" : "文件"}
        </div>
        <div className="min-w-0">
          <div className="flex min-w-0 items-center gap-2">
            <p className="truncate text-sm font-semibold text-slate-950">{file.name}</p>
            {!file.is_dir && <ReplicaReadiness status={file.replica_status} />}
          </div>
          <p className="truncate text-xs text-slate-500">
            {file.is_dir ? "目录" : formatBytes(file.size_bytes)}
            {file.modified_at && ` · 修改于 ${new Date(file.modified_at).toLocaleDateString()}`}
          </p>
        </div>
      </div>
      <div className="min-w-0">
        {file.conflict_of && (
          <p className="mb-2 truncate rounded-md bg-amber-50 px-2 py-1 text-xs text-amber-700">与 {file.conflict_of} 存在冲突</p>
        )}
      </div>
      <div className="grid grid-cols-2 gap-2 sm:flex sm:shrink-0">
        {canPreview && (
          <button
            onClick={() => onPreview(file)}
            className="rounded-lg bg-green-50 px-3 py-2 text-xs font-semibold text-green-700 hover:bg-green-100 active:bg-green-200"
          >
            预览
          </button>
        )}
        {!file.is_dir && (
          <button
            onClick={() => onDownload(file)}
            className="rounded-lg bg-blue-50 px-3 py-2 text-xs font-semibold text-blue-700 hover:bg-blue-100 active:bg-blue-200"
          >
            下载
          </button>
        )}
        {!file.is_dir && !file.conflict_of && (
          <button
            onClick={() => onHistory(file)}
            className="rounded-lg bg-violet-50 px-3 py-2 text-xs font-semibold text-violet-700 hover:bg-violet-100 active:bg-violet-200"
          >
            历史
          </button>
        )}
        <button
          onClick={() => onRename(file)}
          className="rounded-lg bg-slate-100 px-3 py-2 text-xs font-semibold text-slate-700 hover:bg-slate-200 active:bg-slate-300"
        >
          重命名
        </button>
        <button
          onClick={() => onDelete(file)}
          className="rounded-lg bg-red-50 px-3 py-2 text-xs font-semibold text-red-700 hover:bg-red-100 active:bg-red-200"
        >
          删除
        </button>
      </div>
    </div>
  );
}

function TrashCard({ file, busy, onRestore }) {
  return (
    <div className="flex min-w-0 flex-col gap-3 rounded-lg border border-slate-200 bg-white p-4 shadow-sm sm:flex-row sm:items-center">
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className="rounded-md bg-slate-100 px-2 py-1 text-xs font-semibold text-slate-600">{file.is_dir ? "目录" : "文件"}</span>
          <p className="truncate text-sm font-semibold text-slate-950">{file.name}</p>
        </div>
        <p className="mt-2 truncate font-mono text-xs text-slate-500">{file.path}</p>
        <p className="mt-1 text-xs text-slate-500">
          {file.expires_at ? `将在 ${new Date(file.expires_at).toLocaleString()} 后清理` : "等待自动清理"}
        </p>
      </div>
      <button
        onClick={() => onRestore(file)}
        disabled={busy}
        className="rounded-lg bg-blue-600 px-4 py-2.5 text-sm font-semibold text-white hover:bg-blue-700 disabled:opacity-50"
      >
        {busy ? "恢复中..." : "恢复"}
      </button>
    </div>
  );
}

function VersionHistoryDialog({ file, onClose, onRestored }) {
  const [versions, setVersions] = useState([]);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  useEffect(() => {
    let active = true;
    api(`/files/versions?file_id=${encodeURIComponent(file.file_id)}`)
      .then((res) => {
        if (!active) return;
        if (!res.ok) throw new Error(res.error?.message || "加载历史版本失败");
        setVersions(res.data?.entries || []);
      })
      .catch((err) => { if (active) setError(err.message || "加载历史版本失败"); })
      .finally(() => { if (active) setLoading(false); });
    return () => { active = false; };
  }, [file.file_id]);
  const restore = async (version) => {
    setBusy(version.version_id);
    setError("");
    try {
      const res = await api("/files/versions/restore", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ file_id: file.file_id, version_id: version.version_id }),
      });
      if (!res.ok) throw new Error(res.error?.message || "恢复历史版本失败");
      onRestored();
    } catch (err) {
      setError(err.message || "恢复历史版本失败");
    } finally {
      setBusy("");
    }
  };
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-950/45 p-4" onMouseDown={onClose}>
      <div className="max-h-[80vh] w-full max-w-xl overflow-auto rounded-lg border border-slate-200 bg-white p-5 shadow-2xl" onMouseDown={(e) => e.stopPropagation()}>
        <div className="flex items-start justify-between gap-4">
          <div className="min-w-0">
            <h3 className="text-base font-semibold text-slate-950">历史版本</h3>
            <p className="mt-1 truncate text-sm text-slate-500">{file.name}</p>
          </div>
          <button onClick={onClose} className="rounded-lg bg-slate-100 px-3 py-2 text-xs font-semibold text-slate-700 hover:bg-slate-200">关闭</button>
        </div>
        {error && <div className="mt-4"><InlineMessage tone="error">{error}</InlineMessage></div>}
        <div className="mt-4 space-y-2">
          {loading ? (
            <p className="py-6 text-center text-sm text-slate-500">加载中...</p>
          ) : versions.length === 0 ? (
            <p className="py-6 text-center text-sm text-slate-500">没有可恢复的旧版本</p>
          ) : versions.map((version) => (
            <div key={version.version_id} className="flex flex-col gap-3 rounded-lg border border-slate-200 p-3 sm:flex-row sm:items-center">
              <div className="min-w-0 flex-1">
                <p className="text-sm font-semibold text-slate-900">{formatBytes(version.size_bytes)}</p>
                <p className="mt-1 text-xs text-slate-500">被替换于 {new Date(version.superseded_at).toLocaleString()}</p>
                <p className="mt-1 text-xs text-slate-500">可恢复至 {new Date(version.expires_at).toLocaleString()}</p>
              </div>
              <button
                onClick={() => restore(version)}
                disabled={Boolean(busy)}
                className="rounded-lg bg-violet-600 px-4 py-2 text-sm font-semibold text-white hover:bg-violet-700 disabled:opacity-50"
              >
                {busy === version.version_id ? "恢复中..." : "恢复此版本"}
              </button>
            </div>
          ))}
        </div>
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
      .catch(() => { setContent("加载失败"); setLoading(false); });
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
            <a href={url} download className="rounded-lg bg-blue-50 px-3 py-2 text-xs font-semibold text-blue-700 hover:bg-blue-100">下载</a>
            <button onClick={onClose} className="rounded-lg bg-slate-100 px-3 py-2 text-xs font-semibold text-slate-700 hover:bg-slate-200">关闭</button>
          </div>
        </div>
        <div className="flex-1 overflow-auto bg-slate-50 p-4">
          {loading ? (
            <div className="py-8 text-center text-sm text-slate-400">预览加载中...</div>
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

function isInvalidRenamePath(p) {
  if (!p || !p.trim()) return "路径不能为空";
  if (!p.startsWith("/")) return "路径必须以 / 开头";
  if (p === "/") return "不能重命名到根目录";
  if (p.includes("//")) return "路径不能包含连续的 /";
  if (p.length > 1 && p.endsWith("/")) return "路径不能以 / 结尾";
  const base = p.split("/").filter(Boolean).pop() || "";
  if (base === "." || base === "..") return "名称不能是 '.' 或 '..'";
  if (base.startsWith(".")) return "名称不能以点开头（不允许隐藏文件）";
  if (p.includes("/../") || p.endsWith("/..") || p.startsWith("../")) return "路径不能包含 '..'";
  if (p.includes("/./") || p.endsWith("/.") || p.startsWith("./")) return "路径不能包含 '.' 段";
  return null;
}

function RenameDialog({ file, busy, error, onSubmit, onCancel }) {
  const [newPath, setNewPath] = useState(file.path);
  const validationError = isInvalidRenamePath(newPath);
  const canSubmit = newPath && newPath !== file.path && !validationError && !busy;
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-950/45 p-4" onMouseDown={onCancel}>
      <form
        onSubmit={(e) => { e.preventDefault(); if (canSubmit) onSubmit(newPath); }}
        className="w-full max-w-md rounded-lg border border-slate-200 bg-white p-5 shadow-2xl"
        onMouseDown={(e) => e.stopPropagation()}
      >
        <h3 className="text-base font-semibold text-slate-950">重命名项目</h3>
        <p className="mt-1 truncate text-sm text-slate-500">{file.name}</p>
        <label className="mt-4 block">
          <span className="mb-1 block text-xs font-semibold text-slate-500">新的池内路径</span>
          <input
            autoFocus
            type="text"
            value={newPath}
            onChange={(e) => setNewPath(e.target.value)}
            className="w-full rounded-lg border border-slate-300 px-3 py-2 text-sm outline-none focus:border-blue-500 focus:ring-2 focus:ring-blue-100"
          />
        </label>
        {validationError && <div className="mt-3"><InlineMessage tone="error">{validationError}</InlineMessage></div>}
        {error && !validationError && <div className="mt-3"><InlineMessage tone="error">{error}</InlineMessage></div>}
        <div className="mt-5 flex justify-end gap-2">
          <button type="button" onClick={onCancel} className="rounded-lg px-4 py-2 text-sm font-medium text-slate-600 hover:bg-slate-100">取消</button>
          <button type="submit" disabled={!canSubmit} className="rounded-lg bg-blue-600 px-4 py-2 text-sm font-semibold text-white hover:bg-blue-700 disabled:opacity-50">
            {busy ? "重命名中..." : "确认重命名"}
          </button>
        </div>
      </form>
    </div>
  );
}

export default function FilesPage() {
  const [viewMode, setViewMode] = useState("files");
  const [path, setPath] = useState("/");
  const [files, setFiles] = useState([]);
  const [search, setSearch] = useState("");
  const [debouncedSearch, setDebouncedSearch] = useState("");
  const [uploading, setUploading] = useState(false);
  const [uploadProgress, setUploadProgress] = useState(null);
  const [refreshing, setRefreshing] = useState(false);
  const [previewFile, setPreviewFile] = useState(null);
  const [loading, setLoading] = useState(true);
  const [message, setMessage] = useState(null);
  const [renameFile, setRenameFile] = useState(null);
  const [deleteFile, setDeleteFile] = useState(null);
  const [renameBusy, setRenameBusy] = useState(false);
  const [renameError, setRenameError] = useState(null);
  const [deleteBusy, setDeleteBusy] = useState(false);
  const [deleteError, setDeleteError] = useState(null);
  const [restoreBusy, setRestoreBusy] = useState("");
  const [historyFile, setHistoryFile] = useState(null);
  useEffect(() => {
    const id = setTimeout(() => setDebouncedSearch(search), 300);
    return () => clearTimeout(id);
  }, [search]);

  const loadFiles = useCallback(async (silent = false) => {
    if (!silent) setLoading(true);
    try {
      const q = debouncedSearch ? `?q=${encodeURIComponent(debouncedSearch)}` : `?path=${encodeURIComponent(path)}`;
      const res = await api(viewMode === "trash" ? "/trash" : `/files${q}`);
      if (res.ok) {
        setFiles(res.data?.entries || []);
      } else {
        setMessage({ tone: "error", text: res.error?.message || (viewMode === "trash" ? "加载回收站失败" : "加载文件失败") });
      }
    } catch (err) {
      setMessage({ tone: "error", text: err.message || (viewMode === "trash" ? "加载回收站失败" : "加载文件失败") });
    } finally {
      if (!silent) setLoading(false);
    }
  }, [path, debouncedSearch, viewMode]);
  useEffect(() => { loadFiles(); }, [loadFiles]);
  const needsSafetyRefresh = viewMode === "files" && files.some((file) => !file.is_dir && file.replica_status !== "healthy");
  useEffect(() => {
    if (!needsSafetyRefresh) return undefined;
    const id = setInterval(() => loadFiles(true), 3000);
    return () => clearInterval(id);
  }, [needsSafetyRefresh, loadFiles]);
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
      if (xhr.status >= 400) {
        let msg = `Upload failed (HTTP ${xhr.status})`;
        try {
          const data = JSON.parse(xhr.responseText);
          if (data.error?.message) msg = data.error.message;
        } catch { /* non-JSON error body, use default msg */ }
        setMessage({ tone: "error", text: msg });
        loadFiles();
        return;
      }
      try {
        const data = JSON.parse(xhr.responseText);
        if (!data.ok) {
          setMessage({ tone: "error", text: `上传失败：${data.error?.message || "未知错误"}` });
        } else if (data.data?.replica_status === "healthy") {
          setMessage({ tone: "success", text: `已上传 ${file.name}，安全副本已就绪。` });
        } else {
          setMessage({ tone: "warning", text: `已上传 ${file.name}，正在生成安全副本，请暂时保持设备在线。` });
        }
      } catch {
        setMessage({ tone: "error", text: "上传完成，但返回结果异常。" });
      }
      loadFiles();
    };
    xhr.onerror = () => {
      setUploading(false);
      setUploadProgress(null);
      setMessage({ tone: "error", text: "上传失败，请检查 agent 连接后重试。" });
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
    setDeleteBusy(true);
    setDeleteError(null);
    try {
      const res = await api(`/files?path=${encodeURIComponent(deleteFile.path)}`, { method: "DELETE" });
      if (!res.ok) {
        throw new Error(res.error?.message || "删除失败");
      }
      const hours = res.data?.tombstone_retention_hours;
      setMessage({
        tone: "success",
        text: `已删除 ${deleteFile.name}。可在 ${hours || 168} 小时内从回收站恢复。`,
      });
      setDeleteFile(null);
      loadFiles();
    } catch (err) {
      setDeleteError(err.message || "删除失败");
    } finally {
      setDeleteBusy(false);
    }
  };
  const handleRename = async (newPath) => {
    if (!renameFile) return;
    if (!newPath || newPath === renameFile.path) return;
    setRenameBusy(true);
    setRenameError(null);
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
      setMessage({ tone: "success", text: `已重命名 ${renameFile.name}` });
      setRenameFile(null);
      loadFiles();
    } catch (err) {
      setRenameError(err.message || "重命名失败");
    } finally {
      setRenameBusy(false);
    }
  };
  const handleRestore = async (file) => {
    setRestoreBusy(file.file_id);
    setMessage(null);
    try {
      const res = await api("/trash/restore", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ file_id: file.file_id }),
      });
      if (!res.ok) throw new Error(res.error?.message || "恢复失败");
      setMessage({ tone: "success", text: `已恢复 ${file.name}` });
      await loadFiles();
    } catch (err) {
      setMessage({ tone: "error", text: err.message || "恢复失败" });
    } finally {
      setRestoreBusy("");
    }
  };
  const totalSize = files.reduce((sum, f) => sum + (f.is_dir ? 0 : (f.size_bytes || 0)), 0);
  const folders = files.filter((f) => f.is_dir).length;
  return (
    <div className="space-y-4">
      <PageHeader
        eyebrow="存储池"
        title={viewMode === "trash" ? "回收站" : "文件"}
        description={viewMode === "trash" ? "恢复保留期内尚未永久清理的文件和目录。" : "上传、预览、重命名和下载存储池中的文件。"}
        action={
          <div className="flex gap-2">
            <button
              onClick={() => { setViewMode(viewMode === "trash" ? "files" : "trash"); setSearch(""); setMessage(null); }}
              className="rounded-lg bg-slate-100 px-4 py-2.5 text-sm font-semibold text-slate-700 hover:bg-slate-200"
            >
              {viewMode === "trash" ? "返回文件" : "回收站"}
            </button>
            {viewMode === "files" && (
              <label className="block">
                <span className="inline-flex cursor-pointer items-center rounded-lg bg-blue-600 px-4 py-2.5 text-sm font-semibold text-white shadow-sm hover:bg-blue-700 active:bg-blue-800">
                  {uploading ? `上传中 ${uploadProgress ?? 0}%` : "上传文件"}
                </span>
                <input type="file" className="hidden" onChange={handleUpload} disabled={uploading} />
              </label>
            )}
          </div>
        }
      />
      {viewMode === "files" && <div className="grid gap-3 sm:grid-cols-3">
        <div className="rounded-lg border border-slate-200 bg-white p-4 shadow-sm">
          <p className="text-xs font-semibold uppercase text-slate-500">当前项目数</p>
          <p className="mt-1 text-2xl font-semibold text-slate-950">{files.length}</p>
        </div>
        <div className="rounded-lg border border-slate-200 bg-white p-4 shadow-sm">
          <p className="text-xs font-semibold uppercase text-slate-500">文件夹</p>
          <p className="mt-1 text-2xl font-semibold text-slate-950">{folders}</p>
        </div>
        <div className="rounded-lg border border-slate-200 bg-white p-4 shadow-sm">
          <p className="text-xs font-semibold uppercase text-slate-500">当前可见文件大小</p>
          <p className="mt-1 text-2xl font-semibold text-slate-950">{formatBytes(totalSize)}</p>
        </div>
      </div>}
      {viewMode === "files" && <Section>
        <div className="flex flex-col gap-3 lg:flex-row">
          <div className="flex flex-1 items-center rounded-lg border border-slate-300 bg-white px-3 focus-within:border-blue-500 focus-within:ring-2 focus-within:ring-blue-100">
            <span className="text-sm text-slate-400">搜索</span>
            <input
              type="text"
              placeholder="按名称、类型或路径搜索"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              className="min-w-0 flex-1 border-0 bg-transparent px-3 py-3 text-sm outline-none"
            />
            {search && (
              <button onClick={() => setSearch("")} className="rounded-md px-2 py-1 text-xs font-semibold text-slate-500 hover:bg-slate-100">
                清空
              </button>
            )}
          </div>
          <button
            onClick={handleRefresh}
            disabled={refreshing}
            className="rounded-lg bg-slate-100 px-4 py-3 text-sm font-semibold text-slate-700 hover:bg-slate-200 active:bg-slate-300 disabled:opacity-50"
          >
            {refreshing ? "刷新中..." : "刷新"}
          </button>
        </div>
        {uploading && uploadProgress !== null && (
          <div className="mt-4 space-y-2">
            <ProgressBar value={uploadProgress} />
            <p className="text-xs font-medium text-slate-500">正在上传到 {path}</p>
          </div>
        )}
      </Section>}
      {message && <InlineMessage tone={message.tone}>{message.text}</InlineMessage>}
      <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
        {loading ? (
          <EmptyState title="文件加载中..." description="正在读取当前存储池路径。" />
        ) : files.length > 0 ? (
          files.map((f) => viewMode === "trash" ? (
            <TrashCard
              key={f.file_id}
              file={f}
              busy={restoreBusy === f.file_id}
              onRestore={handleRestore}
            />
          ) : (
            <FileCard
              key={f.file_id || f.path}
              file={f}
              onDownload={handleDownload}
              onDelete={(file) => { setDeleteError(null); setDeleteFile(file); }}
              onRename={(file) => { setRenameError(null); setRenameFile(file); }}
              onPreview={setPreviewFile}
              onHistory={setHistoryFile}
            />
          ))
        ) : (
          <EmptyState
            title={viewMode === "trash" ? "回收站是空的" : search ? "没有匹配的文件" : "还没有文件"}
            description={viewMode === "trash" ? "删除的内容会在保留期内显示在这里。" : search ? "试试更短的关键词，或清空搜索条件。" : "上传一个文件，开始往存储池里放内容。"}
          />
        )}
      </div>
      {previewFile && <FilePreview file={previewFile} onClose={() => setPreviewFile(null)} />}
      {historyFile && (
        <VersionHistoryDialog
          file={historyFile}
          onClose={() => setHistoryFile(null)}
          onRestored={() => {
            setHistoryFile(null);
            setMessage({ tone: "success", text: `已将 ${historyFile.name} 恢复为所选版本` });
            loadFiles();
          }}
        />
      )}
      {renameFile && (
        <RenameDialog
          file={renameFile}
          busy={renameBusy}
          error={renameError}
          onCancel={() => { setRenameFile(null); setRenameError(null); }}
          onSubmit={handleRename}
        />
      )}
      {deleteFile && (
        <ConfirmDialog
          title={deleteFile.is_dir ? "删除目录？" : "删除文件？"}
          message={deleteFile.is_dir
            ? `这会把“${deleteFile.name}”及其子内容标记为已删除。相关 Chunk 会按保留期延后回收。`
            : `这会把“${deleteFile.name}”标记为已删除。相关 Chunk 会按保留期延后回收。`}
          confirmLabel="确认删除"
          busy={deleteBusy}
          error={deleteError}
          onCancel={() => { setDeleteFile(null); setDeleteError(null); }}
          onConfirm={handleDelete}
        />
      )}
    </div>
  );
}
