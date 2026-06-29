import { useCallback, useEffect, useState } from "react";
import { API, api } from "../api";
import { formatBytes } from "../utils";

export default function LocalFilesPage() {
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
      <div className="flex items-center gap-2 text-sm text-slate-500 overflow-x-auto">
        {cwd.split("/").filter(Boolean).map((seg, i, arr) => {
          const p = "/" + arr.slice(0, i + 1).join("/");
          return (
            <span key={p} className="flex items-center gap-2 shrink-0">
              <span className="text-slate-300">/</span>
              <button onClick={() => load(p)} className="hover:text-blue-600 truncate max-w-[120px]">{seg}</button>
            </span>
          );
        })}
      </div>

      {/* Up button */}
      {parent && parent !== cwd && (
        <button
          onClick={() => load(parent)}
          className="px-3 py-2 bg-slate-100 rounded-lg text-sm hover:bg-slate-200"
        >
          返回上级
        </button>
      )}

      {/* File list */}
      <div className="grid gap-2 md:grid-cols-2 xl:grid-cols-3">
        {entries.map((e) => (
          <div key={e.path} className="bg-white rounded-lg shadow p-3 flex items-center gap-3">
            <span className="w-8 h-8 rounded bg-slate-100 flex items-center justify-center text-xs font-medium text-slate-500">{e.is_dir ? "D" : "F"}</span>
            <div className="flex-1 min-w-0">
              {e.is_dir ? (
                <button onClick={() => load(e.path)} className="font-medium text-sm text-blue-600 hover:underline truncate block w-full text-left">
                  {e.name}
                </button>
              ) : (
                <p className="font-medium text-sm truncate">{e.name}</p>
              )}
              <p className="text-xs text-slate-400">{e.is_dir ? "目录" : formatBytes(e.size_bytes)}</p>
            </div>
            {!e.is_dir && (
              <button
                onClick={() => { setMigrating(e); setTargetPath("/" + e.name); setDeleteLocal(false); setResult(null); }}
                className="px-3 py-1.5 bg-green-50 text-green-700 rounded-lg text-xs font-medium hover:bg-green-100"
              >
                迁移到存储池
              </button>
            )}
          </div>
        ))}
        {entries.length === 0 && (
          <div className="bg-white rounded-lg shadow p-8 text-center text-slate-400 text-sm col-span-full">
            空目录
          </div>
        )}
      </div>

      {/* Migrate modal */}
      {migrating && (
        <div className="fixed inset-0 bg-black/30 flex items-center justify-center z-50 p-4">
          <div className="bg-white rounded-xl shadow-xl w-full max-w-md p-6 space-y-4">
            <h3 className="font-semibold text-base">迁移到存储池</h3>
            <p className="text-sm text-slate-500">{migrating.name} ({formatBytes(migrating.size_bytes)})</p>
            <label className="block">
              <span className="text-xs text-slate-500 mb-1 block">存储池目标路径</span>
              <input
                type="text"
                value={targetPath}
                onChange={(e) => setTargetPath(e.target.value)}
                className="w-full border rounded-lg px-3 py-2 text-sm"
              />
            </label>
            <label className="flex items-center gap-2 text-sm">
              <input type="checkbox" checked={deleteLocal} onChange={(e) => setDeleteLocal(e.target.checked)} />
              <span>迁移完成后删除本地文件</span>
            </label>
            {result && (
              <div className={`text-sm p-3 rounded-lg ${result.ok ? "bg-green-50 text-green-700" : "bg-red-50 text-red-700"}`}>
                {result.ok ? `迁移完成：${result.data?.chunk_count} 个 Chunk，状态：${result.data?.replica_status}` : result.error?.message}
              </div>
            )}
            <div className="flex gap-2 justify-end">
              <button onClick={() => { setMigrating(null); setResult(null); }} className="px-4 py-2 text-sm text-slate-600 hover:bg-slate-100 rounded-lg">取消</button>
              <button onClick={handleMigrate} disabled={busy} className="px-4 py-2 text-sm bg-blue-600 text-white rounded-lg hover:bg-blue-700 disabled:opacity-50">
                {busy ? "迁移中…" : "开始迁移"}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
