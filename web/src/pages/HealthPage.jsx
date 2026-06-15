import { useCallback, useEffect, useState } from "react";
import { api } from "../api";
import { cx, formatBytes } from "../utils";
import { EmptyState, PageHeader } from "../components/common";

export default function HealthPage() {
  const [summary, setSummary] = useState(null);
  const [chunks, setChunks] = useState([]);
  const [selectedChunk, setSelectedChunk] = useState(null);
  const [loading, setLoading] = useState(true);
  const [page, setPage] = useState(0);
  const pageSize = 100;
  const load = useCallback(async () => {
    setLoading(true);
    try {
      const [sumRes, chunkRes] = await Promise.all([
        api("/health/summary"),
        api(`/health/chunks?limit=${pageSize}&offset=${page * pageSize}`),
      ]);
      if (sumRes.ok) setSummary(sumRes.data);
      if (chunkRes.ok) setChunks(chunkRes.data?.chunks || []);
    } catch {
      // API failure — keep previous state
    } finally {
      setLoading(false);
    }
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
