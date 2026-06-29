import { useEffect, useState } from "react";
import { api } from "./api";
import { cx } from "./utils";
import FilesPage from "./pages/FilesPage";
import MountPage from "./pages/MountPage";
import NodesPage from "./pages/NodesPage";
import HealthPage from "./pages/HealthPage";
import SyncTasksPage from "./pages/SyncTasksPage";
import LogsPage from "./pages/LogsPage";
import { JoinPage, LoginPage } from "./pages/AuthPages";

const navItems = [
  { id: "files", label: "文件", hint: "池内文件" },
  { id: "mount", label: "挂载", hint: "WebDAV" },
  { id: "nodes", label: "节点", hint: "设备" },
  { id: "health", label: "健康", hint: "副本" },
  { id: "tasks", label: "任务", hint: "同步" },
  { id: "logs", label: "日志", hint: "事件" },
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
      setStartupError(err.message || "无法连接本地 agent");
      setLoading(false);
    });
  }, []);

  if (loading) return <div className="flex min-h-screen items-center justify-center text-sm text-slate-400">加载中...</div>;
  if (startupError) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-slate-50 p-4">
        <div className="w-full max-w-md rounded-lg border border-red-200 bg-white p-5 shadow-sm">
          <h1 className="text-lg font-semibold text-slate-950">PocketCluster 无法连接本地 agent</h1>
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
            <p className="hidden text-xs text-slate-500 lg:mt-1 lg:block">局域网存储池</p>
          </div>
          <div className="min-w-0 rounded-lg bg-slate-100 px-3 py-2 text-right lg:mt-5 lg:text-left">
            <p className="text-[10px] font-semibold uppercase text-slate-500">集群</p>
            <p className="max-w-[160px] truncate font-mono text-xs text-slate-700">{clusterId || "未知"}</p>
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
          {tab === "mount" && <MountPage />}
          {tab === "nodes" && <NodesPage />}
          {tab === "health" && <HealthPage />}
          {tab === "tasks" && <SyncTasksPage />}
          {tab === "logs" && <LogsPage />}
        </div>
      </main>

      <nav
        className="fixed bottom-0 left-0 right-0 z-50 border-t border-slate-200 bg-white/95 shadow-[0_-8px_24px_rgba(15,23,42,0.08)] backdrop-blur lg:hidden"
        style={{ paddingBottom: 'max(0.5rem, env(safe-area-inset-bottom))' }}
      >
        <div className="grid grid-cols-6">
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
