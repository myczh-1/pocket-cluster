import { useCallback, useEffect, useState } from "react";
import { api } from "../api";
import { formatBytes, formatLastSeen } from "../utils";
import { EmptyState, InlineMessage, PageHeader, ProgressBar, Section, StatusBadge } from "../components/common";

function NodeCard({ node }) {
  const usedPct = node.total_bytes > 0 ? Math.round((node.used_bytes / node.total_bytes) * 100) : 0;
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
          <span className="shrink-0">Seen {formatLastSeen(node.last_seen)}</span>
        </div>
        <ProgressBar value={usedPct} />
        <div className="flex items-center justify-between">
          <span>{formatBytes(node.used_bytes)} used</span>
          <span>{formatBytes(node.total_bytes)} total</span>
        </div>
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

  const handleSwitch = async (e) => {
    e.preventDefault();
    if (!switchUser || !switchPass) { setSwitchError("Pool username and password are required"); return; }
    setSwitchError(null);
    setSwitching(true);
    try {
      const res = await api("/join", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ bootstrap: switchAddr, pool_user: switchUser, pool_password: switchPass, join_token: switchToken }),
      });
      if (res.ok) window.location.reload();
      else setSwitchError(res.error?.message || "Join failed");
    } catch (err) {
      setSwitchError(err.message || "Network error");
    } finally {
      setSwitching(false);
    }
  };

  const loadNodes = useCallback(() => {
    api("/nodes").then((r) => { if (r.ok) setNodes(r.data || []); });
    api("/join/pending").then((r) => { if (r.ok) setPendingJoins(r.data || []); });
  }, []);

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
        eyebrow="Cluster"
        title="Nodes"
        description="Monitor capacity, approve new devices, and connect this agent to another pool."
        action={
          <button
            onClick={createInvite}
            disabled={creatingInvite}
            className="rounded-lg bg-blue-600 px-4 py-2.5 text-sm font-semibold text-white shadow-sm hover:bg-blue-700 disabled:opacity-50"
          >
            {creatingInvite ? "Creating..." : "Create invite"}
          </button>
        }
      />

      <div className="grid gap-3 md:grid-cols-4">
        <div className="rounded-lg border border-slate-200 bg-white p-4 shadow-sm">
          <p className="text-xs font-semibold uppercase text-slate-500">Nodes</p>
          <p className="mt-1 text-2xl font-semibold text-slate-950">{nodes.length}</p>
        </div>
        <div className="rounded-lg border border-slate-200 bg-white p-4 shadow-sm">
          <p className="text-xs font-semibold uppercase text-slate-500">Online</p>
          <p className="mt-1 text-2xl font-semibold text-green-700">{onlineCount}</p>
        </div>
        <div className="rounded-lg border border-slate-200 bg-white p-4 shadow-sm">
          <p className="text-xs font-semibold uppercase text-slate-500">Used</p>
          <p className="mt-1 text-2xl font-semibold text-slate-950">{formatBytes(usedBytes)}</p>
        </div>
        <div className="rounded-lg border border-slate-200 bg-white p-4 shadow-sm">
          <p className="text-xs font-semibold uppercase text-slate-500">Capacity</p>
          <p className="mt-1 text-2xl font-semibold text-slate-950">{formatBytes(totalBytes)}</p>
        </div>
      </div>
      <Section title="Pool capacity" description={`${usedPct}% used across online and known nodes`}>
        <ProgressBar value={usedPct} tone={usedPct > 80 ? "amber" : "blue"} />
        <div className="mt-2 flex justify-between text-xs text-slate-500">
          <span>{formatBytes(usedBytes)} used</span>
          <span>{formatBytes(Math.max(0, totalBytes - usedBytes))} free</span>
        </div>
      </Section>

      {pendingJoins.length > 0 && (
        <Section title="Pending join requests" description="Review new devices before they join this pool." className="border-amber-200">
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
                  Approve
                </button>
              </div>
            ))}
          </div>
        </Section>
      )}

      <Section title="Invite token" description="Create a one-time token for a nearby device. Tokens expire in 15 minutes.">
        {invite && (
          <div className="rounded-lg bg-slate-50 p-3">
            <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
              <div className="min-w-0">
                <p className="mb-1 text-xs font-semibold uppercase text-slate-500">Join token</p>
                <code className="block break-all font-mono text-sm text-slate-800">{invite.join_token}</code>
                <p className="mt-2 text-xs text-slate-400">Expires {new Date(invite.expires_at).toLocaleString()}</p>
              </div>
              <button onClick={copyInvite} className="rounded-lg bg-white px-3 py-2 text-xs font-semibold text-slate-700 shadow-sm ring-1 ring-slate-200 hover:bg-slate-100">
                {inviteCopied ? "Copied" : "Copy"}
              </button>
            </div>
          </div>
        )}
        {!invite && <EmptyState title="No active invite" description="Create an invite when you are ready to add another device." />}
      </Section>

      <Section
        title="Find nearby pools"
        description="Scan the local network, then use the discovered address to join another pool."
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
                  setScanMessage({ tone: "success", text: `Found ${r.data.nodes.length} node(s). Filled the first address below.` });
                } else {
                  setScanMessage({ tone: "warning", text: "No PocketCluster nodes were found on this network." });
                }
              } catch (e) {
                setScanMessage({ tone: "error", text: e.message || "Scan failed" });
              } finally {
                setScanning(false);
              }
            }}
            disabled={scanning}
            className="rounded-lg bg-slate-100 px-4 py-2 text-sm font-semibold text-slate-700 hover:bg-slate-200 disabled:opacity-50"
          >
            {scanning ? "Scanning..." : "Scan network"}
          </button>
        }
      >
        {scanMessage ? <InlineMessage tone={scanMessage.tone}>{scanMessage.text}</InlineMessage> : <p className="text-sm text-slate-500">Scan uses local discovery and may take a moment on busy Wi-Fi networks.</p>}
      </Section>

      <Section title="Join another pool">
        <button
          onClick={() => setShowSwitch(!showSwitch)}
          className="w-full rounded-lg bg-slate-100 px-3 py-2 text-left text-sm font-semibold text-slate-700 hover:bg-slate-200"
        >
          {showSwitch ? "Hide join form" : "Show join form"}
        </button>
        {showSwitch && (
          <form onSubmit={handleSwitch} className="mt-4 grid gap-3 md:grid-cols-2">
            <input
              type="text"
              value={switchAddr}
              onChange={(e) => setSwitchAddr(e.target.value)}
              placeholder="Pool address (e.g. http://192.168.1.10:7788)"
              required
              className="w-full rounded-lg border border-slate-300 px-4 py-3 text-sm outline-none focus:border-blue-500 focus:ring-2 focus:ring-blue-100 md:col-span-2"
            />
            <input
              type="text"
              value={switchUser}
              onChange={(e) => setSwitchUser(e.target.value)}
              placeholder="Pool username"
              required
              className="w-full rounded-lg border border-slate-300 px-4 py-3 text-sm outline-none focus:border-blue-500 focus:ring-2 focus:ring-blue-100"
            />
            <input
              type="password"
              value={switchPass}
              onChange={(e) => setSwitchPass(e.target.value)}
              placeholder="Pool password"
              required
              className="w-full rounded-lg border border-slate-300 px-4 py-3 text-sm outline-none focus:border-blue-500 focus:ring-2 focus:ring-blue-100"
            />
            <input
              type="text"
              value={switchToken}
              onChange={(e) => setSwitchToken(e.target.value)}
              placeholder="Invite token (optional)"
              className="w-full rounded-lg border border-slate-300 px-4 py-3 text-sm outline-none focus:border-blue-500 focus:ring-2 focus:ring-blue-100 md:col-span-2"
            />
            {switchError && <div className="md:col-span-2"><InlineMessage tone="error">{switchError}</InlineMessage></div>}
            <button
              type="submit"
              disabled={switching || !switchAddr}
              className="rounded-lg bg-blue-600 py-3 text-sm font-semibold text-white hover:bg-blue-700 disabled:opacity-50 md:col-span-2"
            >
              {switching ? "Joining…" : "Switch pool"}
            </button>
          </form>
        )}
      </Section>

      <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
        {nodes.length > 0 ? nodes.map((n) => <NodeCard key={n.node_id} node={n} />) : (
          <EmptyState title="No nodes yet" description="Create a pool or join an existing pool to see devices here." />
        )}
      </div>
    </div>
  );
}
