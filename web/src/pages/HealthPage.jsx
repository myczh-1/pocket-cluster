import { useCallback, useEffect, useState } from "react";
import { api } from "../api";
import { cx, formatBytes, formatLastSeen } from "../utils";
import { EmptyState, PageHeader, StatusBadge } from "../components/common";

function ProgressLine({ value }) {
  const clamped = Math.min(100, Math.max(0, value || 0));
  return (
    <div>
      <div className="h-2 overflow-hidden rounded-full bg-slate-200">
        <div className="h-full rounded-full bg-green-600 transition-all" style={{ width: `${clamped}%` }} />
      </div>
      <p className="mt-1 text-xs font-medium text-slate-500">{clamped}% saved by chunk reuse</p>
    </div>
  );
}

export default function HealthPage() {
  const [summary, setSummary] = useState(null);
  const [insights, setInsights] = useState(null);
  const [chunks, setChunks] = useState([]);
  const [selectedChunk, setSelectedChunk] = useState(null);
  const [loading, setLoading] = useState(true);
  const [page, setPage] = useState(0);
  const pageSize = 100;
  const load = useCallback(async () => {
    setLoading(true);
    try {
      const [sumRes, insightsRes, chunkRes] = await Promise.all([
        api("/health/summary"),
        api("/health/insights"),
        api(`/health/chunks?limit=${pageSize}&offset=${page * pageSize}`),
      ]);
      if (sumRes.ok) setSummary(sumRes.data);
      if (insightsRes.ok) setInsights(insightsRes.data);
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
  const storage = insights?.storage;
  const repair = insights?.repair;
  const risk = insights?.risk;
  const riskyFiles = risk?.files || [];
  const riskyNodes = risk?.nodes || [];
  const dedupPercent = storage?.dedup_ratio ? Math.round(storage.dedup_ratio * 100) : 0;
  return (
    <div className="space-y-5">
      <PageHeader
        eyebrow="Replication"
        title="Health"
        description="Check whether pool data is safe, how much storage is saved, and what repair is doing next."
      />
      {insights && (
        <div className="grid gap-3 lg:grid-cols-3">
          <div className="rounded-lg border border-slate-200 bg-white p-4 shadow-sm">
            <div className="text-xs font-semibold uppercase text-slate-500">Storage efficiency</div>
            <div className="mt-1 text-2xl font-bold text-slate-950">{formatBytes(storage?.dedup_saved_bytes || 0)}</div>
            <p className="mt-1 text-xs leading-5 text-slate-500">
              Saved across {storage?.file_count || 0} file(s). Logical {formatBytes(storage?.logical_bytes || 0)}, stored chunks {formatBytes(storage?.unique_chunk_bytes || 0)}.
            </p>
            <div className="mt-3">
              <ProgressLine value={dedupPercent} />
            </div>
          </div>
          <div className={`rounded-lg border p-4 shadow-sm ${risk?.affected_file_count > 0 ? "border-amber-200 bg-amber-50" : "border-green-200 bg-green-50"}`}>
            <div className="text-xs font-semibold uppercase text-slate-500">Files at risk</div>
            <div className={`mt-1 text-2xl font-bold ${risk?.affected_file_count > 0 ? "text-amber-700" : "text-green-700"}`}>
              {risk?.affected_file_count || 0}
            </div>
            <p className="mt-1 text-xs leading-5 text-slate-600">
              {risk?.affected_file_count > 0 ? "Some files reference chunks that need attention." : "No files currently reference unhealthy chunks."}
            </p>
          </div>
          <div className="rounded-lg border border-slate-200 bg-white p-4 shadow-sm">
            <div className="text-xs font-semibold uppercase text-slate-500">Repair loop</div>
            <div className="mt-1 text-lg font-bold capitalize text-slate-950">{repair?.status || "idle"}</div>
            <p className="mt-1 text-xs leading-5 text-slate-500">{repair?.message || "Replica coverage is currently stable."}</p>
            <div className="mt-3 grid grid-cols-2 gap-2 text-xs text-slate-500">
              <span>Queued: <strong className="text-slate-800">{repair?.queued_chunks || 0}</strong></span>
              <span>Repairing: <strong className="text-slate-800">{repair?.repairing_chunks || 0}</strong></span>
            </div>
          </div>
        </div>
      )}
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
        {repair?.next_retry_seconds > 0 && <> · Next repair pass: <span className="font-medium text-slate-700">about {repair.next_retry_seconds}s</span></>}
      </div>
      {risk?.affected_file_count > 0 && (
        <div className="rounded-lg border border-amber-200 bg-white p-4 shadow-sm">
          <div className="mb-2 text-sm font-semibold text-slate-950">Affected files</div>
          <div className="space-y-1">
            {(risk.affected_files || []).slice(0, 6).map((p) => (
              <div key={p} className="truncate rounded-md bg-amber-50 px-2 py-1 font-mono text-xs text-amber-800">{p}</div>
            ))}
            {risk.affected_file_count > 6 && (
              <p className="text-xs text-slate-500">And {risk.affected_file_count - 6} more file(s).</p>
            )}
          </div>
        </div>
      )}
      <div className="grid gap-4 xl:grid-cols-2">
        <div className="rounded-lg border border-slate-200 bg-white p-4 shadow-sm">
          <div className="mb-3 flex items-center justify-between">
            <h3 className="text-sm font-semibold text-slate-950">Files needing attention</h3>
            <span className="text-xs text-slate-500">{riskyFiles.length} file(s)</span>
          </div>
          <div className="space-y-2">
            {riskyFiles.slice(0, 8).map((file) => (
              <div key={file.file_id} className="rounded-lg border border-slate-200 bg-slate-50 p-3">
                <div className="flex items-start justify-between gap-3">
                  <div className="min-w-0">
                    <div className="truncate font-mono text-xs text-slate-600">{file.path}</div>
                    <div className="mt-1 text-xs text-slate-500">
                      {file.readable_chunks}/{file.chunk_count} readable
                      {file.unavailable_chunks > 0 && ` · ${file.unavailable_chunks} unavailable`}
                      {file.under_replicated_chunks > 0 && ` · ${file.under_replicated_chunks} under-replicated`}
                    </div>
                  </div>
                  <StatusBadge status={file.status} />
                </div>
              </div>
            ))}
            {riskyFiles.length === 0 && (
              <EmptyState title="No risky files" description="Every known file currently has healthy replica coverage." />
            )}
          </div>
        </div>
        <div className="rounded-lg border border-slate-200 bg-white p-4 shadow-sm">
          <div className="mb-3 flex items-center justify-between">
            <h3 className="text-sm font-semibold text-slate-950">Node contribution</h3>
            <span className="text-xs text-slate-500">{riskyNodes.length} node(s)</span>
          </div>
          <div className="space-y-2">
            {riskyNodes.slice(0, 8).map((node) => (
              <div key={node.node_id} className="rounded-lg border border-slate-200 bg-slate-50 p-3">
                <div className="flex items-start justify-between gap-3">
                  <div className="min-w-0">
                    <div className="truncate text-sm font-semibold text-slate-950">{node.name || node.node_id}</div>
                    <div className="mt-1 text-xs text-slate-500">
                      {node.platform} · Seen {formatLastSeen(node.last_seen)}
                    </div>
                    <div className="mt-1 text-xs text-slate-500">
                      {node.chunk_count} chunk(s) · {node.risk_chunk_count} risky · {node.repairing_chunks} repairing
                    </div>
                    <div className="mt-1 text-xs text-slate-500">
                      {formatBytes(node.used_bytes || 0)} used / {formatBytes(node.total_bytes || 0)} total
                    </div>
                  </div>
                  <StatusBadge status={node.status} />
                </div>
              </div>
            ))}
            {riskyNodes.length === 0 && (
              <EmptyState title="No nodes tracked" description="Node health details will appear after trusted nodes join the pool." />
            )}
          </div>
        </div>
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
