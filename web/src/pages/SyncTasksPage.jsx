import { useCallback, useEffect, useState } from "react";
import { api } from "../api";
import { EmptyState, InlineMessage, PageHeader, Section, StatusBadge } from "../components/common";

function formatWhen(value) {
  if (!value) return "从未";
  return new Date(value).toLocaleString();
}

function kindLabel(kind) {
  switch (kind) {
    case "upload":
      return "上传";
    case "metadata_pull":
      return "拉取元数据";
    case "metadata_push":
      return "推送元数据";
    case "replica_repair":
      return "副本修复";
    case "integrity_check":
      return "完整性校验";
    case "retention_purge":
      return "立即清理保留数据";
    default:
      return kind || "任务";
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
        setMessage({ tone: "error", text: res.error?.message || `启动${kind}失败` });
      }
    } catch (err) {
      setMessage({ tone: "error", text: err.message || `启动${kind}失败` });
    } finally {
      setRunningJob("");
    }
  };

  return (
    <div className="space-y-5">
      <PageHeader
        eyebrow="运维"
        title="同步任务"
        description="查看当前 agent 正在上传、同步、修复、重试或阻塞中的工作。"
        action={
          <div className="flex flex-wrap gap-2">
            <button
              onClick={() => runJob("/jobs/rescan", "重新扫描", "已启动健康重扫任务。")}
              disabled={runningJob === "rescan"}
              className="rounded-lg bg-slate-100 px-4 py-2.5 text-sm font-semibold text-slate-700 hover:bg-slate-200 disabled:opacity-50"
            >
              {runningJob === "rescan" ? "启动中..." : "执行重扫"}
            </button>
            <button
              onClick={() => runJob("/jobs/repair-under-replicated", "修复", "已启动副本不足 Chunk 的修复任务。")}
              disabled={runningJob === "repair"}
              className="rounded-lg bg-blue-600 px-4 py-2.5 text-sm font-semibold text-white hover:bg-blue-700 disabled:opacity-50"
            >
              {runningJob === "repair" ? "启动中..." : "修复副本不足"}
            </button>
            <button
              onClick={() => runJob("/jobs/integrity-check", "完整性校验", "已启动完整性校验任务。")}
              disabled={runningJob === "integrity"}
              className="rounded-lg bg-emerald-600 px-4 py-2.5 text-sm font-semibold text-white hover:bg-emerald-700 disabled:opacity-50"
            >
              {runningJob === "integrity" ? "启动中..." : "执行完整性校验"}
            </button>
          </div>
        }
      />

      {message && <InlineMessage tone={message.tone}>{message.text}</InlineMessage>}

      <div className="grid grid-cols-2 gap-3 md:grid-cols-6">
        {["running", "retrying", "blocked", "failed", "done", "pending"].map((status) => (
          <div key={status} className="rounded-lg border border-slate-200 bg-white p-4 shadow-sm">
            <div className="text-xs font-semibold uppercase text-slate-500">{status === "running" ? "运行中" : status === "retrying" ? "重试中" : status === "blocked" ? "已阻塞" : status === "failed" ? "失败" : status === "done" ? "完成" : "等待中"}</div>
            <div className="mt-1 text-lg font-bold text-slate-950">{grouped[status] || 0}</div>
          </div>
        ))}
      </div>

      <Section title="最近任务" description="这里优先显示由操作者主动触发的后台动作。">
        {loading ? (
          <div className="py-10 text-center text-sm text-slate-400">任务加载中...</div>
        ) : jobs.length === 0 ? (
          <EmptyState title="还没有任务" description="执行一次重扫、修复或完整性校验后，这里会出现记录。" />
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
                    <div>创建于：<span className="font-medium text-slate-700">{formatWhen(job.created_at)}</span></div>
                    <div className="mt-1">更新于：<span className="font-medium text-slate-700">{formatWhen(job.updated_at)}</span></div>
                    {job.finished_at && (
                      <div className="mt-1">完成于：<span className="font-medium text-slate-700">{formatWhen(job.finished_at)}</span></div>
                    )}
                  </div>
                </div>
              </div>
            ))}
          </div>
        )}
      </Section>

      <Section title="最近同步任务" description="任务按最近更新时间排序。">
        {loading ? (
          <div className="py-10 text-center text-sm text-slate-400">同步任务加载中...</div>
        ) : tasks.length === 0 ? (
          <EmptyState title="还没有同步任务" description="上传、元数据同步或副本修复发生后，这里会显示任务。" />
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
                    <div>开始于：<span className="font-medium text-slate-700">{formatWhen(task.started_at)}</span></div>
                    <div className="mt-1">更新于：<span className="font-medium text-slate-700">{formatWhen(task.updated_at)}</span></div>
                    {task.finished_at && (
                      <div className="mt-1">完成于：<span className="font-medium text-slate-700">{formatWhen(task.finished_at)}</span></div>
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
