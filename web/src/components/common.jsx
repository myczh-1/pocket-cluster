import { cx } from "../utils";

const statusLabels = {
  healthy: "健康",
  under_replicated: "副本不足",
  unavailable: "不可用",
  repairing: "修复中",
  online: "在线",
  offline: "离线",
  pending: "等待中",
  running: "运行中",
  retrying: "重试中",
  blocked: "已阻塞",
  failed: "失败",
  done: "完成",
  idle: "空闲",
};

export function statusLabel(status) {
  return statusLabels[status] || status || "-";
}

export function StatusBadge({ status }) {
  const colors = {
    healthy: "border-green-200 bg-green-50 text-green-700",
    under_replicated: "border-amber-200 bg-amber-50 text-amber-700",
    unavailable: "border-red-200 bg-red-50 text-red-700",
    repairing: "border-blue-200 bg-blue-50 text-blue-700",
    online: "border-green-200 bg-green-50 text-green-700",
    offline: "border-slate-200 bg-slate-100 text-slate-600",
    pending: "border-slate-200 bg-slate-100 text-slate-700",
    running: "border-blue-200 bg-blue-50 text-blue-700",
    retrying: "border-amber-200 bg-amber-50 text-amber-700",
    blocked: "border-red-200 bg-red-50 text-red-700",
    failed: "border-red-200 bg-red-50 text-red-700",
    done: "border-green-200 bg-green-50 text-green-700",
  };
  return (
    <span className={cx(
      "inline-flex items-center rounded-full border px-2.5 py-1 text-xs font-semibold capitalize",
      colors[status] || "border-slate-200 bg-slate-100 text-slate-600"
    )}>
      {statusLabel(status)}
    </span>
  );
}

export function PageHeader({ title, description, eyebrow, action }) {
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

export function Section({ title, description, action, children, className = "" }) {
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

export function InlineMessage({ tone = "info", children }) {
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

export function EmptyState({ title, description, action }) {
  return (
    <div className="col-span-full rounded-lg border border-dashed border-slate-300 bg-slate-50 px-4 py-10 text-center">
      <p className="text-sm font-semibold text-slate-700">{title}</p>
      {description && <p className="mx-auto mt-1 max-w-md text-sm text-slate-500">{description}</p>}
      {action && <div className="mt-4">{action}</div>}
    </div>
  );
}

export function ProgressBar({ value, tone = "blue" }) {
  const color = tone === "green" ? "bg-green-600" : tone === "amber" ? "bg-amber-500" : "bg-blue-600";
  return (
    <div className="h-2 w-full overflow-hidden rounded-full bg-slate-200">
      <div className={cx("h-full rounded-full transition-all", color)} style={{ width: `${Math.min(100, Math.max(0, value))}%` }} />
    </div>
  );
}

export function ConfirmDialog({ title, message, confirmLabel = "确认", tone = "danger", busy, onConfirm, onCancel }) {
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
            取消
          </button>
          <button type="button" onClick={onConfirm} disabled={busy} className={cx("rounded-lg px-4 py-2 text-sm font-semibold disabled:opacity-50", confirmClass)}>
            {busy ? "处理中..." : confirmLabel}
          </button>
        </div>
      </div>
    </div>
  );
}
