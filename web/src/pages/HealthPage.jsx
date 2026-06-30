import { useCallback, useEffect, useState } from "react";
import { api } from "../api";
import { cx, formatBytes, formatLastSeen } from "../utils";
import { EmptyState, PageHeader, StatusBadge, statusLabel } from "../components/common";

function ProgressLine({ value }) {
  const clamped = Math.min(100, Math.max(0, value || 0));
  return (
    <div>
      <div className="h-2 overflow-hidden rounded-full bg-slate-200">
        <div className="h-full rounded-full bg-green-600 transition-all" style={{ width: `${clamped}%` }} />
      </div>
      <p className="mt-1 text-xs font-medium text-slate-500">通过 Chunk 复用节省了 {clamped}% 空间</p>
    </div>
  );
}

export default function HealthPage() {
  const [summary, setSummary] = useState(null);
  const [insights, setInsights] = useState(null);
  const [chunks, setChunks] = useState([]);
  const [selectedChunk, setSelectedChunk] = useState(null);
  const [showRetainedChunks, setShowRetainedChunks] = useState(false);
  const [retentionHours, setRetentionHours] = useState("168");
  const [savingRetention, setSavingRetention] = useState(false);
  const [retentionSaved, setRetentionSaved] = useState(false);
  const [purgingRetained, setPurgingRetained] = useState(false);
  const [purgeDone, setPurgeDone] = useState(false);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [page, setPage] = useState(0);
  const pageSize = 100;
  const load = useCallback(async ({ background = false } = {}) => {
    if (background) {
      setRefreshing(true);
    } else {
      setLoading(true);
    }
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
      setRefreshing(false);
    }
  }, [page]);
  useEffect(() => { load(); }, [load]);
  // Auto-refresh every 10 seconds
  useEffect(() => {
    const id = setInterval(() => load({ background: true }), 10_000);
    return () => clearInterval(id);
  }, [load]);
  const storage = insights?.storage;
  const repair = insights?.repair;
  const risk = insights?.risk;
  const coverage = insights?.coverage;
  const riskyFiles = risk?.files || [];
  const riskyNodes = risk?.nodes || [];
  const visibleChunks = showRetainedChunks ? chunks : chunks.filter((c) => (c.referencing_files || []).length > 0);
  const retainedChunks = chunks.filter((c) => (c.referencing_files || []).length === 0);
  const dedupPercent = storage?.dedup_ratio ? Math.round(storage.dedup_ratio * 100) : 0;
  useEffect(() => {
    if (storage?.tombstone_retention_hours) {
      setRetentionHours(String(storage.tombstone_retention_hours));
    }
  }, [storage?.tombstone_retention_hours]);
  useEffect(() => {
    if (!selectedChunk) return;
    const updated = chunks.find((chunk) => chunk.chunk_id === selectedChunk.chunk_id);
    setSelectedChunk(updated || null);
  }, [chunks, selectedChunk]);

  if (loading) return <div className="py-16 text-center text-sm text-slate-400">健康数据加载中...</div>;
  if (!summary) return <div className="py-16 text-center text-sm text-slate-400">健康数据暂不可用</div>;
  const statusColor = {
    healthy: "border-green-200 bg-green-50 text-green-700",
    under_replicated: "border-amber-200 bg-amber-50 text-amber-700",
    unavailable: "border-red-200 bg-red-50 text-red-700",
    repairing: "border-blue-200 bg-blue-50 text-blue-700",
  };

  async function saveRetention() {
    const hours = Number(retentionHours);
    if (!Number.isFinite(hours) || hours < 1) return;
    setSavingRetention(true);
    setRetentionSaved(false);
    try {
      const res = await api("/settings", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ tombstone_retention_hours: hours }),
      });
      if (res.ok) {
        await load({ background: true });
        setRetentionSaved(true);
        setTimeout(() => setRetentionSaved(false), 1400);
      }
    } finally {
      setSavingRetention(false);
    }
  }

  async function purgeRetainedNow() {
    setPurgingRetained(true);
    setPurgeDone(false);
    try {
      const res = await api("/jobs/purge-retained-data", { method: "POST" });
      if (res.ok) {
        await load({ background: true });
        setPurgeDone(true);
        setTimeout(() => setPurgeDone(false), 1600);
      }
    } finally {
      setPurgingRetained(false);
    }
  }
  return (
    <div className="space-y-5">
      <PageHeader
        eyebrow="副本"
        title="健康"
        description="查看当前数据是否安全、节省了多少空间，以及修复流程下一步在做什么。"
      />
      {insights && (
        <div className="grid gap-3 lg:grid-cols-3">
          <div className="rounded-lg border border-slate-200 bg-white p-4 shadow-sm">
            <div className="text-xs font-semibold uppercase text-slate-500">空间效率</div>
            <div className="mt-1 text-2xl font-bold text-slate-950">{formatBytes(storage?.dedup_saved_bytes || 0)}</div>
            <p className="mt-1 text-xs leading-5 text-slate-500">
              仅统计活文件。覆盖 {storage?.file_count || 0} 个文件，逻辑大小 {formatBytes(storage?.logical_bytes || 0)}，唯一 Chunk 存储 {formatBytes(storage?.unique_chunk_bytes || 0)}，物理副本占用 {formatBytes(storage?.physical_replica_bytes || 0)}。
            </p>
            <div className="mt-3">
              <ProgressLine value={dedupPercent} />
            </div>
          </div>
          <div className={`rounded-lg border p-4 shadow-sm ${risk?.affected_file_count > 0 ? "border-amber-200 bg-amber-50" : "border-green-200 bg-green-50"}`}>
            <div className="text-xs font-semibold uppercase text-slate-500">风险文件</div>
            <div className={`mt-1 text-2xl font-bold ${risk?.affected_file_count > 0 ? "text-amber-700" : "text-green-700"}`}>
              {risk?.affected_file_count || 0}
            </div>
            <p className="mt-1 text-xs leading-5 text-slate-600">
              {risk?.affected_file_count > 0 ? "有些文件引用了需要关注的 Chunk。" : "当前没有文件引用异常 Chunk。"}
            </p>
          </div>
          <div className="rounded-lg border border-slate-200 bg-white p-4 shadow-sm">
            <div className="text-xs font-semibold uppercase text-slate-500">修复循环</div>
            <div className="mt-1 text-lg font-bold capitalize text-slate-950">{statusLabel(repair?.status || "idle")}</div>
            <p className="mt-1 text-xs leading-5 text-slate-500">{repair?.message || "当前副本覆盖状态稳定。"}</p>
            <div className="mt-3 grid grid-cols-2 gap-2 text-xs text-slate-500">
              <span>等待中：<strong className="text-slate-800">{repair?.queued_chunks || 0}</strong></span>
              <span>修复中：<strong className="text-slate-800">{repair?.repairing_chunks || 0}</strong></span>
            </div>
          </div>
        </div>
      )}
      {insights && (
        <div className="grid gap-3 lg:grid-cols-2">
          <div className="rounded-lg border border-amber-200 bg-amber-50 p-4 shadow-sm">
            <div className="text-xs font-semibold uppercase text-amber-700">待回收占用</div>
            <div className="mt-1 text-2xl font-bold text-amber-800">{formatBytes(storage?.retained_physical_replica_bytes || 0)}</div>
            <p className="mt-1 text-xs leading-5 text-amber-700">
              这些 Chunk 当前没有被活文件引用，通常来自已删除文件的保留期。现有 {storage?.retained_unique_chunk_count || 0} 个待回收唯一 Chunk，{storage?.retained_physical_replica_count || 0} 个物理副本。
            </p>
            <div className="mt-3">
              <button
                onClick={purgeRetainedNow}
                disabled={purgingRetained}
                className={`rounded-lg px-4 py-2 text-sm font-semibold transition ${
                  purgeDone
                    ? "bg-green-600 text-white"
                    : "bg-amber-600 text-white hover:bg-amber-700"
                } disabled:opacity-50`}
              >
                {purgingRetained ? "清理中..." : purgeDone ? "已提交 ✓" : "立即清理待回收 Chunk"}
              </button>
            </div>
          </div>
          <div className="rounded-lg border border-slate-200 bg-white p-4 shadow-sm">
            <div className="text-xs font-semibold uppercase text-slate-500">删除保留期</div>
            <div className="mt-1 flex items-end gap-2">
              <input
                type="number"
                min="1"
                max="2160"
                value={retentionHours}
                onChange={(e) => setRetentionHours(e.target.value)}
                className="w-24 rounded-md border border-slate-300 px-3 py-2 text-sm text-slate-900"
              />
              <span className="pb-2 text-sm text-slate-500">小时</span>
              <button
                onClick={saveRetention}
                disabled={savingRetention}
                className={`rounded-lg px-4 py-2 text-sm font-semibold transition ${
                  retentionSaved
                    ? "bg-green-600 text-white"
                    : "bg-slate-100 text-slate-700 hover:bg-slate-200"
                } disabled:opacity-50`}
              >
                {savingRetention ? "保存中..." : retentionSaved ? "已保存 ✓" : "保存"}
              </button>
            </div>
            <p className="mt-2 text-xs leading-5 text-slate-500">
              删除文件后不会立即删 Chunk。超过这个保留期，后台清理轮询会回收未被引用的 Chunk。
            </p>
          </div>
        </div>
      )}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
        <div className={`rounded-lg border p-4 shadow-sm ${statusColor[summary.overall_status] || "border-slate-200 bg-white text-slate-700"}`}>
          <div className="text-xs font-semibold uppercase opacity-70">总体状态</div>
          <div className="mt-1 text-lg font-bold capitalize">{statusLabel(summary.overall_status)}</div>
        </div>
        <div className="rounded-lg border border-slate-200 bg-white p-4 shadow-sm">
          <div className="text-xs font-semibold uppercase text-slate-500">文件</div>
          <div className="mt-1 text-lg font-bold text-slate-950">{summary.total_files}</div>
        </div>
        <div className="rounded-lg border border-slate-200 bg-white p-4 shadow-sm">
          <div className="text-xs font-semibold uppercase text-slate-500">唯一 Chunk 数</div>
          <div className="mt-1 text-lg font-bold text-slate-950">{storage?.unique_chunk_count ?? coverage?.total_chunks ?? summary.total_chunks}</div>
        </div>
        <div className="rounded-lg border border-slate-200 bg-white p-4 shadow-sm">
          <div className="text-xs font-semibold uppercase text-slate-500">物理副本数</div>
          <div className="mt-1 text-lg font-bold text-slate-950">{storage?.physical_replica_count ?? 0}</div>
        </div>
      </div>
      <div className="grid grid-cols-4 gap-3">
        <div className="rounded-lg border border-slate-200 bg-white p-4 shadow-sm">
          <div className="text-xs font-semibold uppercase text-slate-500">健康</div>
          <div className="mt-1 text-lg font-bold text-green-700">{coverage?.healthy_chunks ?? summary.healthy_chunks}</div>
        </div>
        <div className="rounded-lg border border-slate-200 bg-white p-4 shadow-sm">
          <div className="text-xs font-semibold uppercase text-slate-500">副本不足</div>
          <div className={`mt-1 text-lg font-bold ${(coverage?.under_replicated_chunks ?? summary.under_replicated_chunks) > 0 ? "text-amber-600" : "text-slate-400"}`}>
            {coverage?.under_replicated_chunks ?? summary.under_replicated_chunks}
          </div>
        </div>
        <div className="rounded-lg border border-slate-200 bg-white p-4 shadow-sm">
          <div className="text-xs font-semibold uppercase text-slate-500">不可用</div>
          <div className={`mt-1 text-lg font-bold ${(coverage?.unavailable_chunks ?? summary.unavailable_chunks) > 0 ? "text-red-600" : "text-slate-400"}`}>
            {coverage?.unavailable_chunks ?? summary.unavailable_chunks}
          </div>
        </div>
        <div className="rounded-lg border border-slate-200 bg-white p-4 shadow-sm">
          <div className="text-xs font-semibold uppercase text-slate-500">修复中</div>
          <div className={`mt-1 text-lg font-bold ${(coverage?.repairing_chunks ?? summary.repairing_chunks) > 0 ? "text-blue-600" : "text-slate-400"}`}>
            {coverage?.repairing_chunks ?? summary.repairing_chunks}
          </div>
        </div>
      </div>
      <div className="rounded-lg border border-slate-200 bg-white px-4 py-3 text-xs text-slate-500 shadow-sm">
        最近扫描：<span className="font-medium text-slate-700">{summary.last_scan_at ? new Date(summary.last_scan_at).toLocaleString() : "从未"}</span>
        {summary.last_repair_at && <> · 最近修复：<span className="font-medium text-slate-700">{new Date(summary.last_repair_at).toLocaleString()}</span></>}
        {repair?.next_retry_seconds > 0 && <> · 下一轮修复：<span className="font-medium text-slate-700">约 {repair.next_retry_seconds} 秒后</span></>}
        {refreshing && <> · <span className="font-medium text-slate-700">刷新中...</span></>}
      </div>
      {risk?.affected_file_count > 0 && (
        <div className="rounded-lg border border-amber-200 bg-white p-4 shadow-sm">
          <div className="mb-2 text-sm font-semibold text-slate-950">受影响文件</div>
          <div className="space-y-1">
            {(risk.affected_files || []).slice(0, 6).map((p) => (
              <div key={p} className="truncate rounded-md bg-amber-50 px-2 py-1 font-mono text-xs text-amber-800">{p}</div>
            ))}
            {risk.affected_file_count > 6 && (
              <p className="text-xs text-slate-500">还有 {risk.affected_file_count - 6} 个文件。</p>
            )}
          </div>
        </div>
      )}
      <div className="grid gap-4 xl:grid-cols-2">
        <div className="rounded-lg border border-slate-200 bg-white p-4 shadow-sm">
          <div className="mb-3 flex items-center justify-between">
            <h3 className="text-sm font-semibold text-slate-950">需要关注的文件</h3>
            <span className="text-xs text-slate-500">{riskyFiles.length} 个文件</span>
          </div>
          <div className="space-y-2">
            {riskyFiles.slice(0, 8).map((file) => (
              <div key={file.file_id} className="rounded-lg border border-slate-200 bg-slate-50 p-3">
                <div className="flex items-start justify-between gap-3">
                  <div className="min-w-0">
                    <div className="truncate font-mono text-xs text-slate-600">{file.path}</div>
                    <div className="mt-1 text-xs text-slate-500">
                      {file.readable_chunks}/{file.chunk_count} 可读
                      {file.unavailable_chunks > 0 && ` · ${file.unavailable_chunks} 不可用`}
                      {file.under_replicated_chunks > 0 && ` · ${file.under_replicated_chunks} 副本不足`}
                    </div>
                  </div>
                  <StatusBadge status={file.status} />
                </div>
              </div>
            ))}
            {riskyFiles.length === 0 && (
              <EmptyState title="没有风险文件" description="当前所有已知文件的副本覆盖都正常。" />
            )}
          </div>
        </div>
        <div className="rounded-lg border border-slate-200 bg-white p-4 shadow-sm">
          <div className="mb-3 flex items-center justify-between">
            <h3 className="text-sm font-semibold text-slate-950">节点贡献</h3>
            <span className="text-xs text-slate-500">{riskyNodes.length} 个节点</span>
          </div>
          <div className="space-y-2">
            {riskyNodes.slice(0, 8).map((node) => (
              <div key={node.node_id} className="rounded-lg border border-slate-200 bg-slate-50 p-3">
                <div className="flex items-start justify-between gap-3">
                  <div className="min-w-0">
                    <div className="truncate text-sm font-semibold text-slate-950">{node.name || node.node_id}</div>
                    <div className="mt-1 text-xs text-slate-500">
                      {node.platform} · 最近在线 {formatLastSeen(node.last_seen)}
                    </div>
                    <div className="mt-1 text-xs text-slate-500">
                      {node.replica_count} 个副本 · {node.risk_chunk_count} 个风险 · {node.repairing_chunks} 个修复中
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
              <EmptyState title="还没有节点记录" description="可信节点加入存储池后，这里会出现节点健康详情。" />
            )}
          </div>
        </div>
      </div>
      {selectedChunk && (
        <div className="rounded-lg border border-blue-200 bg-white p-4 shadow-sm">
          <div className="mb-3 flex items-center justify-between">
            <h3 className="text-sm font-semibold text-slate-950">Chunk 详情</h3>
            <button onClick={() => setSelectedChunk(null)} className="rounded-md px-2 py-1 text-sm text-slate-400 hover:bg-slate-100 hover:text-slate-700">关闭</button>
          </div>
          <div className="mb-2 break-all font-mono text-xs text-slate-500">{selectedChunk.chunk_id}</div>
          <div className="mb-3 grid grid-cols-2 gap-2 text-sm text-slate-700">
            <div>大小：{formatBytes(selectedChunk.size_bytes)}</div>
            <div>副本：{selectedChunk.online_replicas}/{selectedChunk.target_replicas}</div>
            <div>状态：<span className={`font-medium ${selectedChunk.status === "healthy" ? "text-green-600" : selectedChunk.status === "unavailable" ? "text-red-600" : "text-yellow-600"}`}>{statusLabel(selectedChunk.status)}</span></div>
            <div>用途：{(selectedChunk.referencing_files || []).length > 0 ? "活文件引用中" : "未被活文件引用，等待回收"}</div>
          </div>
          {selectedChunk.replica_nodes && selectedChunk.replica_nodes.length > 0 && (
            <div>
              <div className="mb-1 text-xs font-semibold text-slate-500">副本节点</div>
              <div className="space-y-1">
                {selectedChunk.replica_nodes.map((r) => (
                  <div key={r.node_id} className="flex items-center gap-2 text-xs">
                    <span className={`w-2 h-2 rounded-full ${r.online ? "bg-green-500" : "bg-gray-300"}`}></span>
                    <span className="font-mono">{r.node_id}</span>
                    <span className="text-slate-400">{r.online ? "在线" : "离线"}</span>
                  </div>
                ))}
              </div>
            </div>
          )}
          {selectedChunk.referencing_files && selectedChunk.referencing_files.length > 0 && (
            <div className="mt-3">
              <div className="mb-1 text-xs font-semibold text-slate-500">引用该 Chunk 的文件</div>
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
        <div className="mb-3 flex items-center justify-between gap-3">
          <h3 className="text-sm font-semibold text-slate-950">活文件 Chunks（{visibleChunks.length}）</h3>
          <button
            onClick={() => setShowRetainedChunks((v) => !v)}
            className="rounded-lg bg-slate-100 px-3 py-1.5 text-xs font-semibold text-slate-700 hover:bg-slate-200"
          >
            {showRetainedChunks ? "隐藏待回收 Chunk" : `显示待回收 Chunk（${retainedChunks.length}）`}
          </button>
        </div>
        <div className="space-y-2">
          {visibleChunks.map((c) => (
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
                  {statusLabel(c.status)}
                </span>
              </div>
              <div className="mt-1 flex gap-4 text-xs text-slate-400">
                <span>{formatBytes(c.size_bytes)}</span>
                <span>{c.online_replicas}/{c.target_replicas} 副本</span>
                {(c.referencing_files || []).length > 0 ? <span>{c.referencing_files.length} 个文件</span> : <span>待回收</span>}
              </div>
            </button>
          ))}
          {visibleChunks.length === 0 && (
            <EmptyState title="还没有 Chunk" description="存储池里有文件后，这里会显示健康数据。" />
          )}
        </div>
      </div>
    </div>
  );
}
