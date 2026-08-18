import { useCallback, useEffect, useState } from "react";
import { api } from "../api";
import { formatBytes, formatLastSeen } from "../utils";
import { EmptyState, InlineMessage, PageHeader, ProgressBar, Section, StatusBadge } from "../components/common";

function NodeCard({ node, evacuation, evacuating, onEvacuate }) {
  const usedPct = node.total_bytes > 0 ? Math.round((node.used_bytes / node.total_bytes) * 100) : 0;
  const safeToExit = evacuation?.safe_to_exit;
  const blocked = evacuation?.state === "blocked";
  const offline = node.status !== "online";
  const missingDestinations = evacuation
    ? Math.max(0, evacuation.required_other_replicas - evacuation.eligible_destination_nodes)
    : 0;
  const evacuationTone = offline
    ? "bg-slate-100 text-slate-700"
    : safeToExit
      ? "bg-emerald-50 text-emerald-700"
      : blocked
        ? "bg-amber-50 text-amber-700"
        : "bg-blue-50 text-blue-700";
  return (
    <div className="rounded-lg border border-slate-200 bg-white p-4 shadow-sm">
      <div className="mb-3 flex items-start justify-between gap-3">
        <div className="min-w-0">
          <h3 className="truncate text-sm font-semibold text-slate-950">{node.name || node.node_id.slice(0, 8)}</h3>
          <p className="mt-1 truncate text-xs text-slate-500">{node.platform} · {node.node_id.slice(0, 8)}</p>
        </div>
        <StatusBadge status={node.status} />
      </div>
      <div className="space-y-2 text-xs text-slate-500">
        <div className="flex items-center justify-between gap-3">
          <span className="truncate">{node.address}</span>
          <span className="shrink-0">最近在线 {formatLastSeen(node.last_seen)}</span>
        </div>
        <ProgressBar value={usedPct} />
        <div className="flex items-center justify-between">
          <span>已用 {formatBytes(node.used_bytes)}</span>
          <span>总计 {formatBytes(node.total_bytes)}</span>
        </div>
        {evacuation && (
          <div className={`mt-3 rounded-lg p-3 ${evacuationTone}`}>
            <p className="font-semibold">
              {offline
                ? "设备已离线"
                : safeToExit
                  ? "可以安全退出"
                  : blocked
                    ? "还不能退出"
                    : evacuating
                      ? "正在迁出数据"
                      : "需要迁出数据"}
            </p>
            <p className="mt-1 leading-5">
              {offline
                ? safeToExit
                  ? "其他在线设备已有完整安全副本，不需要再执行迁出。"
                  : `仍有 ${evacuation.pending_chunks} 个数据块未安全迁出，请先让这台设备上线。`
                : safeToExit
                  ? `本设备的 ${evacuation.total_chunks} 个数据块均已在其他设备留有安全副本。`
                  : blocked
                    ? `还需连接 ${missingDestinations} 台设备，才能迁出剩余 ${evacuation.pending_chunks} 个数据块。`
                    : `已有 ${evacuation.safe_chunks}/${evacuation.total_chunks} 个数据块完成迁出。`}
            </p>
            {!offline && !safeToExit && !blocked && (
              <button
                onClick={() => onEvacuate(node)}
                disabled={evacuating}
                className="mt-2 w-full rounded-md bg-white px-3 py-2 font-semibold shadow-sm ring-1 ring-current/15 hover:bg-white/70 disabled:cursor-not-allowed disabled:opacity-60"
              >
                {evacuating ? "迁移中..." : "迁出这台设备的数据"}
              </button>
            )}
          </div>
        )}
      </div>
    </div>
  );
}

export default function NodesPage() {
  const [nodes, setNodes] = useState([]);
  const [pendingJoins, setPendingJoins] = useState([]);
  const [invite, setInvite] = useState(null);
  const [inviteCopied, setInviteCopied] = useState(false);
  const [creatingInvite, setCreatingInvite] = useState(false);
  const [showSwitch, setShowSwitch] = useState(false);
  const [switchAddr, setSwitchAddr] = useState("");
  const [switchUser, setSwitchUser] = useState("");
  const [switchPass, setSwitchPass] = useState("");
  const [switchToken, setSwitchToken] = useState("");
  const [switching, setSwitching] = useState(false);
  const [switchError, setSwitchError] = useState(null);
  const [scanning, setScanning] = useState(false);
  const [scanMessage, setScanMessage] = useState(null);
  const [evacuation, setEvacuation] = useState({});
  const [evacuationJob, setEvacuationJob] = useState(null);
  const [nodeActionMessage, setNodeActionMessage] = useState(null);

  const handleSwitch = async (e) => {
    e.preventDefault();
    if (!switchUser || !switchPass) { setSwitchError("存储池用户名和密码不能为空"); return; }
    setSwitchError(null);
    setSwitching(true);
    try {
      const res = await api("/join", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ bootstrap: switchAddr, pool_user: switchUser, pool_password: switchPass, join_token: switchToken }),
      });
      if (res.ok) window.location.reload();
      else setSwitchError(res.error?.message || "加入失败");
    } catch (err) {
      setSwitchError(err.message || "网络错误");
    } finally {
      setSwitching(false);
    }
  };

  const loadNodes = useCallback(async () => {
    const [nodeResult, pendingResult, evacuationResult] = await Promise.all([
      api("/nodes"),
      api("/join/pending"),
      api("/nodes/evacuation"),
    ]);
    if (nodeResult.ok) setNodes(nodeResult.data || []);
    if (pendingResult.ok) setPendingJoins(pendingResult.data || []);
    if (evacuationResult.ok) {
      setEvacuation(Object.fromEntries((evacuationResult.data?.nodes || []).map((item) => [item.node_id, item])));
    }
  }, []);

  const evacuateNode = async (node) => {
    setNodeActionMessage(null);
    try {
      const result = await api(`/nodes/${node.node_id}/evacuate`, { method: "POST" });
      if (!result.ok) {
        setNodeActionMessage({ tone: "error", text: result.error?.message || "无法开始迁移。" });
        return;
      }
      setEvacuationJob({ id: result.data.id, nodeId: node.node_id });
      setNodeActionMessage({ tone: "success", text: `正在为 ${node.name || node.node_id.slice(0, 8)} 准备安全退出。` });
    } catch (error) {
      setNodeActionMessage({ tone: "error", text: error.message || "无法开始迁移。" });
    }
  };

  const approveJoin = async (nodeId) => {
    try {
      const r = await api(`/join/approve/${nodeId}`, { method: "POST" });
      if (r.ok) loadNodes();
    } catch (e) {
      console.error("Approve failed:", e);
    }
  };

  useEffect(() => {
    loadNodes();
    const id = setInterval(loadNodes, 3000);
    return () => clearInterval(id);
  }, [loadNodes]);

  useEffect(() => {
    if (!evacuationJob) return undefined;
    const id = setInterval(async () => {
      const result = await api(`/jobs/${evacuationJob.id}`);
      if (!result.ok || !["done", "blocked", "failed"].includes(result.data?.status)) return;
      clearInterval(id);
      await loadNodes();
      if (result.data.status === "done") {
        setNodeActionMessage({ tone: "success", text: "迁移和校验完成，这台设备现在可以安全退出。" });
      } else if (result.data.status === "blocked") {
        setNodeActionMessage({ tone: "warning", text: "迁移尚未完成，请保持设备在线并检查其他节点。" });
      } else {
        setNodeActionMessage({ tone: "error", text: result.data.error || "迁移失败，请检查同步任务。" });
      }
      setEvacuationJob(null);
    }, 2000);
    return () => clearInterval(id);
  }, [evacuationJob, loadNodes]);

  const totalBytes = nodes.reduce((s, n) => s + (n.total_bytes || 0), 0);
  const usedBytes = nodes.reduce((s, n) => s + (n.used_bytes || 0), 0);
  const onlineCount = nodes.filter((n) => n.status === "online").length;
  const usedPct = totalBytes > 0 ? Math.round((usedBytes / totalBytes) * 100) : 0;

  const createInvite = async () => {
    setCreatingInvite(true);
    setInviteCopied(false);
    try {
      const res = await api("/invites", { method: "POST" });
      if (res.ok) setInvite(res.data);
    } finally {
      setCreatingInvite(false);
    }
  };

  const copyInvite = async () => {
    if (!invite?.join_token) return;
    try {
      await navigator.clipboard.writeText(invite.join_token);
      setInviteCopied(true);
      setTimeout(() => setInviteCopied(false), 1800);
    } catch {
      setInviteCopied(false);
    }
  };

  return (
    <div className="space-y-4">
      <PageHeader
        eyebrow="集群"
        title="节点"
        description="查看容量、批准新设备加入，并让当前 agent 连接到另一个存储池。"
        action={
          <button
            onClick={createInvite}
            disabled={creatingInvite}
            className="rounded-lg bg-blue-600 px-4 py-2.5 text-sm font-semibold text-white shadow-sm hover:bg-blue-700 disabled:opacity-50"
          >
            {creatingInvite ? "创建中..." : "创建邀请"}
          </button>
        }
      />

      {nodeActionMessage && <InlineMessage tone={nodeActionMessage.tone}>{nodeActionMessage.text}</InlineMessage>}

      <div className="grid gap-3 md:grid-cols-4">
        <div className="rounded-lg border border-slate-200 bg-white p-4 shadow-sm">
          <p className="text-xs font-semibold uppercase text-slate-500">节点数</p>
          <p className="mt-1 text-2xl font-semibold text-slate-950">{nodes.length}</p>
        </div>
        <div className="rounded-lg border border-slate-200 bg-white p-4 shadow-sm">
          <p className="text-xs font-semibold uppercase text-slate-500">在线</p>
          <p className="mt-1 text-2xl font-semibold text-green-700">{onlineCount}</p>
        </div>
        <div className="rounded-lg border border-slate-200 bg-white p-4 shadow-sm">
          <p className="text-xs font-semibold uppercase text-slate-500">已用</p>
          <p className="mt-1 text-2xl font-semibold text-slate-950">{formatBytes(usedBytes)}</p>
        </div>
        <div className="rounded-lg border border-slate-200 bg-white p-4 shadow-sm">
          <p className="text-xs font-semibold uppercase text-slate-500">容量</p>
          <p className="mt-1 text-2xl font-semibold text-slate-950">{formatBytes(totalBytes)}</p>
        </div>
      </div>
      <Section title="存储池容量" description={`在线和已知节点总计已使用 ${usedPct}%`}>
        <ProgressBar value={usedPct} tone={usedPct > 80 ? "amber" : "blue"} />
        <div className="mt-2 flex justify-between text-xs text-slate-500">
          <span>已用 {formatBytes(usedBytes)}</span>
          <span>剩余 {formatBytes(Math.max(0, totalBytes - usedBytes))}</span>
        </div>
      </Section>

      {pendingJoins.length > 0 && (
        <Section title="待批准加入请求" description="新设备加入当前存储池前，需要先在这里审核。" className="border-amber-200">
          <div className="space-y-2">
            {pendingJoins.map((pj) => (
              <div key={pj.node_id} className="flex flex-col gap-3 rounded-lg bg-amber-50 p-3 sm:flex-row sm:items-center sm:justify-between">
                <div className="min-w-0">
                  <p className="truncate text-sm font-semibold text-slate-950">{pj.name || pj.node_id.slice(0, 8)}</p>
                  <p className="truncate text-xs text-slate-500">{pj.platform} / {pj.address}</p>
                </div>
                <button
                  onClick={() => approveJoin(pj.node_id)}
                  className="rounded-lg bg-green-600 px-3 py-2 text-xs font-semibold text-white hover:bg-green-700"
                >
                  批准
                </button>
              </div>
            ))}
          </div>
        </Section>
      )}

      <Section title="邀请令牌" description="为附近设备创建一次性令牌。令牌 15 分钟后过期。">
        {invite && (
          <div className="rounded-lg bg-slate-50 p-3">
            <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
              <div className="min-w-0">
                <p className="mb-1 text-xs font-semibold uppercase text-slate-500">加入令牌</p>
                <code className="block break-all font-mono text-sm text-slate-800">{invite.join_token}</code>
                <p className="mt-2 text-xs text-slate-400">过期时间 {new Date(invite.expires_at).toLocaleString()}</p>
              </div>
              <button onClick={copyInvite} className="rounded-lg bg-white px-3 py-2 text-xs font-semibold text-slate-700 shadow-sm ring-1 ring-slate-200 hover:bg-slate-100">
                {inviteCopied ? "已复制" : "复制"}
              </button>
            </div>
          </div>
        )}
        {!invite && <EmptyState title="当前没有邀请" description="准备添加新设备时，再创建邀请即可。" />}
      </Section>

      <Section
        title="查找附近存储池"
        description="扫描当前局域网，然后使用发现到的地址加入另一个存储池。"
        action={
          <button
            onClick={async () => {
              setScanning(true);
              setScanMessage(null);
              try {
                const r = await api("/network/scan");
                if (r.ok && r.data?.nodes?.length > 0) {
                  const firstNode = r.data.nodes[0];
                  setSwitchAddr(`http://${firstNode.address}`);
                  setShowSwitch(true);
                  setScanMessage({ tone: "success", text: `找到 ${r.data.nodes.length} 个节点，已自动填入第一个地址。` });
                } else {
                  setScanMessage({ tone: "warning", text: "当前网络中没有发现 PocketCluster 节点。" });
                }
              } catch (e) {
                setScanMessage({ tone: "error", text: e.message || "扫描失败" });
              } finally {
                setScanning(false);
              }
            }}
            disabled={scanning}
            className="rounded-lg bg-slate-100 px-4 py-2 text-sm font-semibold text-slate-700 hover:bg-slate-200 disabled:opacity-50"
          >
            {scanning ? "扫描中..." : "扫描网络"}
          </button>
        }
      >
        {scanMessage ? <InlineMessage tone={scanMessage.tone}>{scanMessage.text}</InlineMessage> : <p className="text-sm text-slate-500">扫描依赖本地发现，在繁忙 Wi-Fi 网络中可能需要一点时间。</p>}
      </Section>

      <Section title="加入另一个存储池">
        <button
          onClick={() => setShowSwitch(!showSwitch)}
          className="w-full rounded-lg bg-slate-100 px-3 py-2 text-left text-sm font-semibold text-slate-700 hover:bg-slate-200"
        >
          {showSwitch ? "收起加入表单" : "展开加入表单"}
        </button>
        {showSwitch && (
          <form onSubmit={handleSwitch} className="mt-4 grid gap-3 md:grid-cols-2">
            <input
              type="text"
              value={switchAddr}
              onChange={(e) => setSwitchAddr(e.target.value)}
              placeholder="存储池地址（例如 http://192.168.1.10:7788）"
              required
              className="w-full rounded-lg border border-slate-300 px-4 py-3 text-sm outline-none focus:border-blue-500 focus:ring-2 focus:ring-blue-100 md:col-span-2"
            />
            <input
              type="text"
              value={switchUser}
              onChange={(e) => setSwitchUser(e.target.value)}
              placeholder="存储池用户名"
              required
              className="w-full rounded-lg border border-slate-300 px-4 py-3 text-sm outline-none focus:border-blue-500 focus:ring-2 focus:ring-blue-100"
            />
            <input
              type="password"
              value={switchPass}
              onChange={(e) => setSwitchPass(e.target.value)}
              placeholder="存储池密码"
              required
              className="w-full rounded-lg border border-slate-300 px-4 py-3 text-sm outline-none focus:border-blue-500 focus:ring-2 focus:ring-blue-100"
            />
            <input
              type="text"
              value={switchToken}
              onChange={(e) => setSwitchToken(e.target.value)}
              placeholder="邀请令牌（可选）"
              className="w-full rounded-lg border border-slate-300 px-4 py-3 text-sm outline-none focus:border-blue-500 focus:ring-2 focus:ring-blue-100 md:col-span-2"
            />
            {switchError && <div className="md:col-span-2"><InlineMessage tone="error">{switchError}</InlineMessage></div>}
            <button
              type="submit"
              disabled={switching || !switchAddr}
              className="rounded-lg bg-blue-600 py-3 text-sm font-semibold text-white hover:bg-blue-700 disabled:opacity-50 md:col-span-2"
            >
              {switching ? "加入中…" : "切换存储池"}
            </button>
          </form>
        )}
      </Section>

      <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
        {nodes.length > 0 ? nodes.map((n) => (
          <NodeCard
            key={n.node_id}
            node={n}
            evacuation={evacuation[n.node_id]}
            evacuating={evacuationJob?.nodeId === n.node_id}
            onEvacuate={evacuateNode}
          />
        )) : (
          <EmptyState title="还没有节点" description="创建存储池或加入已有存储池后，这里会显示设备。" />
        )}
      </div>
    </div>
  );
}
