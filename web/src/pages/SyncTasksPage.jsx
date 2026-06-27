import { useCallback, useEffect, useState } from "react";
import { api } from "../api";
import { EmptyState, InlineMessage, PageHeader, Section, StatusBadge } from "../components/common";

function formatWhen(value) {
  if (!value) return "never";
  return new Date(value).toLocaleString();
}

function kindLabel(kind) {
  switch (kind) {
    case "upload":
      return "Upload";
    case "metadata_pull":
      return "Metadata Pull";
    case "metadata_push":
      return "Metadata Push";
    case "replica_repair":
      return "Replica Repair";
    case "integrity_check":
      return "Integrity Check";
    default:
      return kind || "Task";
  }
}

export default function SyncTasksPage() {
  const [tasks, setTasks] = useState([]);
  const [jobs, setJobs] = useState([]);
  const [loading, setLoading] = useState(true);
  const [runningJob, setRunningJob] = useState("");
  const [message, setMessage] = useState(null);

  const load = useCallback(async () => {
    try {
      const [tasksRes, jobsRes] = await Promise.all([
        api("/sync/tasks"),
        api("/jobs"),
      ]);
      if (tasksRes.ok) {
        setTasks(tasksRes.data?.tasks || []);
      }
      if (jobsRes.ok) {
        setJobs(jobsRes.data?.jobs || []);
      }
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  useEffect(() => {
    const id = setInterval(load, 5000);
    return () => clearInterval(id);
  }, [load]);

  const grouped = tasks.reduce((acc, task) => {
    acc[task.status] = (acc[task.status] || 0) + 1;
    return acc;
  }, {});

  const runJob = async (path, kind, successText) => {
    setRunningJob(kind);
    setMessage(null);
    try {
      const res = await api(path, { method: "POST" });
      if (res.ok) {
        setMessage({ tone: "success", text: successText });
        await load();
      } else {
        setMessage({ tone: "error", text: res.error?.message || `Failed to start ${kind}` });
      }
    } catch (err) {
      setMessage({ tone: "error", text: err.message || `Failed to start ${kind}` });
    } finally {
      setRunningJob("");
    }
  };

  return (
    <div className="space-y-5">
      <PageHeader
        eyebrow="Operations"
        title="Sync Tasks"
        description="See what the agent is currently uploading, syncing, repairing, retrying, or blocking on."
        action={
          <div className="flex flex-wrap gap-2">
            <button
              onClick={() => runJob("/jobs/rescan", "rescan", "Started a health rescan job.")}
              disabled={runningJob === "rescan"}
              className="rounded-lg bg-slate-100 px-4 py-2.5 text-sm font-semibold text-slate-700 hover:bg-slate-200 disabled:opacity-50"
            >
              {runningJob === "rescan" ? "Starting..." : "Run rescan"}
            </button>
            <button
              onClick={() => runJob("/jobs/repair-under-replicated", "repair", "Started a repair job for under-replicated chunks.")}
              disabled={runningJob === "repair"}
              className="rounded-lg bg-blue-600 px-4 py-2.5 text-sm font-semibold text-white hover:bg-blue-700 disabled:opacity-50"
            >
              {runningJob === "repair" ? "Starting..." : "Repair under-replicated"}
            </button>
          </div>
        }
      />

      {message && <InlineMessage tone={message.tone}>{message.text}</InlineMessage>}

      <div className="grid grid-cols-2 gap-3 md:grid-cols-6">
        {["running", "retrying", "blocked", "failed", "done", "pending"].map((status) => (
          <div key={status} className="rounded-lg border border-slate-200 bg-white p-4 shadow-sm">
            <div className="text-xs font-semibold uppercase text-slate-500">{status}</div>
            <div className="mt-1 text-lg font-bold text-slate-950">{grouped[status] || 0}</div>
          </div>
        ))}
      </div>

      <Section title="Recent jobs" description="Operator-triggered background actions are listed here first.">
        {loading ? (
          <div className="py-10 text-center text-sm text-slate-400">Loading jobs...</div>
        ) : jobs.length === 0 ? (
          <EmptyState title="No jobs yet" description="Start a rescan or repair job to create an operator action record." />
        ) : (
          <div className="space-y-3">
            {jobs.map((job) => (
              <div key={job.id} className="rounded-lg border border-slate-200 bg-slate-50/60 p-4">
                <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
                  <div className="min-w-0">
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="text-sm font-semibold text-slate-950">{job.title || job.kind}</span>
                      <StatusBadge status={job.status} />
                      <span className="rounded-full bg-slate-200 px-2 py-1 text-[11px] font-semibold uppercase tracking-wide text-slate-600">
                        {job.kind}
                      </span>
                    </div>
                    {job.message && <p className="mt-2 text-sm leading-6 text-slate-600">{job.message}</p>}
                    {job.error && <p className="mt-2 rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">{job.error}</p>}
                  </div>
                  <div className="shrink-0 text-xs text-slate-500">
                    <div>Created: <span className="font-medium text-slate-700">{formatWhen(job.created_at)}</span></div>
                    <div className="mt-1">Updated: <span className="font-medium text-slate-700">{formatWhen(job.updated_at)}</span></div>
                    {job.finished_at && (
                      <div className="mt-1">Finished: <span className="font-medium text-slate-700">{formatWhen(job.finished_at)}</span></div>
                    )}
                  </div>
                </div>
              </div>
            ))}
          </div>
        )}
      </Section>

      <Section title="Recent tasks" description="Tasks are ordered by the latest update time.">
        {loading ? (
          <div className="py-10 text-center text-sm text-slate-400">Loading sync tasks...</div>
        ) : tasks.length === 0 ? (
          <EmptyState title="No sync tasks yet" description="Tasks will appear here after uploads, metadata sync, or replica repair activity." />
        ) : (
          <div className="space-y-3">
            {tasks.map((task) => (
              <div key={task.id} className="rounded-lg border border-slate-200 bg-slate-50/60 p-4">
                <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
                  <div className="min-w-0">
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="text-sm font-semibold text-slate-950">{task.title || kindLabel(task.kind)}</span>
                      <StatusBadge status={task.status} />
                      <span className="rounded-full bg-slate-200 px-2 py-1 text-[11px] font-semibold uppercase tracking-wide text-slate-600">
                        {kindLabel(task.kind)}
                      </span>
                    </div>
                    {task.target && (
                      <div className="mt-2 break-all font-mono text-xs text-slate-500">{task.target}</div>
                    )}
                    {task.message && (
                      <p className="mt-2 text-sm leading-6 text-slate-600">{task.message}</p>
                    )}
                    {task.error && (
                      <p className="mt-2 rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">{task.error}</p>
                    )}
                  </div>
                  <div className="shrink-0 text-xs text-slate-500">
                    <div>Started: <span className="font-medium text-slate-700">{formatWhen(task.started_at)}</span></div>
                    <div className="mt-1">Updated: <span className="font-medium text-slate-700">{formatWhen(task.updated_at)}</span></div>
                    {task.finished_at && (
                      <div className="mt-1">Finished: <span className="font-medium text-slate-700">{formatWhen(task.finished_at)}</span></div>
                    )}
                  </div>
                </div>
              </div>
            ))}
          </div>
        )}
      </Section>
    </div>
  );
}
