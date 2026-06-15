import { useEffect, useState } from "react";
import { API, api } from "../api";

export function JoinPage({ mode }) {
  const [bootstrap, setBootstrap] = useState("");
  const [token, setToken] = useState("");
  const [joinUser, setJoinUser] = useState("");
  const [joinPass, setJoinPass] = useState("");
  const [createUser, setCreateUser] = useState("");
  const [createPass, setCreatePass] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState(null);
  const [discovered, setDiscovered] = useState([]);
  const [selectedAddr, setSelectedAddr] = useState("");
  const [scanning, setScanning] = useState(false);
  const [scanResults, setScanResults] = useState([]);
  const [showCreate, setShowCreate] = useState(false);

  useEffect(() => {
    if (mode === "invite") {
      const poll = setInterval(async () => {
        try {
          const r = await api("/nodes/discovered");
          if (r.ok) setDiscovered(r.data || []);
        } catch {}
      }, 3000);
      return () => clearInterval(poll);
    }
  }, [mode]);

  const handleScan = async () => {
    setScanning(true);
    setScanResults([]);
    try {
      const r = await api("/network/scan");
      if (r.ok) setScanResults(r.data?.nodes || []);
    } catch (e) {
      setError(e.message);
    } finally {
      setScanning(false);
    }
  };
  const handleCreateCluster = async (e) => {
    e.preventDefault();
    if (!createUser || !createPass) { setError("Username and password are required"); return; }
    setLoading(true);
    setError(null);
    try {
      const r = await api("/cluster", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ username: createUser, password: createPass }),
      });
      if (r.ok) window.location.reload();
      else setError(r.error?.message || "Failed to create cluster");
    } catch (e) {
      setError(e.message);
    } finally {
      setLoading(false);
    }
  };

  const handleJoin = async (e) => {
    e.preventDefault();
    if (!joinUser || !joinPass) { setError("Pool username and password are required"); return; }
    setLoading(true);
    setError(null);
    try {
      const addr = selectedAddr || bootstrap;
      const r = await api("/join", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ bootstrap: addr, join_token: token, pool_user: joinUser, pool_password: joinPass }),
      });
      if (r.ok) window.location.reload();
      else setError(r.error?.message || "Join failed");
    } catch (e) {
      setError(e.message);
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="min-h-screen flex items-center justify-center p-4">
      <div className="w-full max-w-sm">
        <div className="text-center mb-8">
          <h1 className="text-2xl font-bold mb-2">PocketCluster</h1>
          <p className="text-gray-500 text-sm">Distributed storage pool</p>
        </div>

        {mode === "invite" && discovered.length > 0 && (
          <div className="bg-white rounded-lg shadow p-4 mb-4">
            <h2 className="font-semibold text-sm mb-3">Discovered nodes</h2>
            <div className="space-y-2">
              {discovered.map((n) => (
                <button
                  key={n.node_id}
                  onClick={() => setSelectedAddr(`http://${n.address}`)}
                  className={`w-full text-left p-3 rounded-lg border ${
                    selectedAddr === `http://${n.address}` ? "border-blue-500 bg-blue-50" : "border-gray-200"
                  }`}
                >
                  <p className="font-medium text-sm">{n.name || n.node_id.slice(0, 8)}</p>
                  <p className="text-xs text-gray-500">{n.platform} · {n.address}</p>
                </button>
              ))}
            </div>
          </div>
        )}

        {/* Network scan */}
        <div className="bg-white rounded-lg shadow p-4 mb-4">
          <div className="flex items-center justify-between mb-3">
            <h2 className="font-semibold text-sm">Scan local network</h2>
            <button
              onClick={handleScan}
              disabled={scanning}
              className="px-3 py-1.5 bg-gray-100 rounded text-sm hover:bg-gray-200 disabled:opacity-50"
            >
              {scanning ? "Scanning..." : "Scan"}
            </button>
          </div>
          {scanResults.length > 0 && (
            <div className="space-y-2">
              {scanResults.map((n) => (
                <button
                  key={n.node_id}
                  onClick={() => {
                    setSelectedAddr(`http://${n.address}`);
                    setBootstrap(`http://${n.address}`);
                  }}
                  className={`w-full text-left p-3 rounded-lg border ${
                    selectedAddr === `http://${n.address}` ? "border-blue-500 bg-blue-50" : "border-gray-200"
                  }`}
                >
                  <p className="font-medium text-sm">{n.node_id.slice(0, 8)}…</p>
                  <p className="text-xs text-gray-500">{n.address}</p>
                </button>
              ))}
            </div>
          )}
          {scanResults.length === 0 && !scanning && (
            <p className="text-xs text-gray-400">Click scan to find PocketCluster nodes on your network</p>
          )}
        </div>

        <form onSubmit={handleJoin} className="bg-white rounded-lg shadow p-4 mb-4 space-y-3">
          <h2 className="font-semibold text-sm">Join existing pool</h2>
          <input
            type="text"
            value={bootstrap}
            onChange={(e) => setBootstrap(e.target.value)}
            placeholder="Pool address (e.g. http://192.168.1.10:7788)"
            className="w-full border rounded-lg px-4 py-3 text-sm"
          />
          <input
            type="text"
            value={joinUser}
            onChange={(e) => setJoinUser(e.target.value)}
            placeholder="Pool username"
            required
            className="w-full border rounded-lg px-4 py-3 text-sm"
          />
          <input
            type="password"
            value={joinPass}
            onChange={(e) => setJoinPass(e.target.value)}
            placeholder="Pool password"
            required
            className="w-full border rounded-lg px-4 py-3 text-sm"
          />
          <input
            type="text"
            value={token}
            onChange={(e) => setToken(e.target.value)}
            placeholder="Invite token (optional)"
            className="w-full border rounded-lg px-4 py-3 text-sm"
          />
          {error && <p className="text-sm text-red-600">{error}</p>}
          <button
            type="submit"
            disabled={loading || (!bootstrap && !selectedAddr)}
            className="w-full bg-blue-600 text-white py-3 rounded-lg text-sm font-medium hover:bg-blue-700 disabled:opacity-50"
          >
            {loading ? "Joining…" : "Join"}
          </button>
        </form>
        <div className="bg-white rounded-lg shadow p-4">
          <button
            onClick={() => setShowCreate(!showCreate)}
            className="text-sm text-gray-500 hover:text-gray-700 w-full text-left"
          >
            {showCreate ? "Hide" : "Create a new pool"}...
          </button>
          {showCreate && (
            <form onSubmit={handleCreateCluster} className="mt-3 space-y-3">
              <input
                type="text"
                value={createUser}
                onChange={(e) => setCreateUser(e.target.value)}
                placeholder="Pool username"
                required
                className="w-full border rounded-lg px-4 py-3 text-sm"
              />
              <input
                type="password"
                value={createPass}
                onChange={(e) => setCreatePass(e.target.value)}
                placeholder="Pool password"
                required
                className="w-full border rounded-lg px-4 py-3 text-sm"
              />
              {error && <p className="text-sm text-red-600">{error}</p>}
              <button
                type="submit"
                disabled={loading || !createUser || !createPass}
                className="w-full bg-blue-600 text-white py-3 rounded-lg text-sm font-medium hover:bg-blue-700 disabled:opacity-50"
              >
                {loading ? "Creating..." : "Create Pool"}
              </button>
            </form>
          )}
        </div>
      </div>
    </div>
  );
}
export function LoginPage() {
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState(null);
  const [busy, setBusy] = useState(false);

  const handleSubmit = async (e) => {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const res = await fetch(`${API}/auth/login`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        credentials: "same-origin",
        body: JSON.stringify({ username, password }),
      });
      const data = await res.json();
      if (data.ok) {
        window.location.reload();
      } else {
        setError(data.error?.message || "Login failed");
      }
    } catch (err) {
      setError(err.message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="min-h-screen flex items-center justify-center p-4">
      <div className="w-full max-w-sm">
        <div className="text-center mb-8">
          <h1 className="text-2xl font-bold mb-2">PocketCluster</h1>
          <p className="text-gray-500 text-sm">Login to your storage pool</p>
        </div>
        <form onSubmit={handleSubmit} className="bg-white rounded-lg shadow p-4 space-y-3">
          <input
            type="text"
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            placeholder="Username"
            required
            className="w-full border rounded-lg px-4 py-3 text-sm"
          />
          <input
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            placeholder="Password"
            required
            className="w-full border rounded-lg px-4 py-3 text-sm"
          />
          {error && <p className="text-sm text-red-600">{error}</p>}
          <button
            type="submit"
            disabled={busy || !username || !password}
            className="w-full bg-blue-600 text-white py-3 rounded-lg text-sm font-medium hover:bg-blue-700 disabled:opacity-50"
          >
            {busy ? "Logging in..." : "Login"}
          </button>
        </form>
      </div>
    </div>
  );
}
