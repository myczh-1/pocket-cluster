import { useCallback, useEffect, useState } from "react";
import { api } from "../api";
import { EmptyState, PageHeader, Section } from "../components/common";

export default function LogsPage() {
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
