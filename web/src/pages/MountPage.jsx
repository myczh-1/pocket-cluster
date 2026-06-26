import { useEffect, useMemo, useState } from "react";
import { api } from "../api";
import { EmptyState, InlineMessage, PageHeader, Section } from "../components/common";

function CopyButton({ value, label = "Copy" }) {
  const [copied, setCopied] = useState(false);
  const copy = async () => {
    if (!value) return;
    try {
      await navigator.clipboard.writeText(value);
      setCopied(true);
      setTimeout(() => setCopied(false), 1600);
    } catch {
      setCopied(false);
    }
  };
  return (
    <button
      onClick={copy}
      disabled={!value}
      className="rounded-lg bg-slate-100 px-3 py-2 text-xs font-semibold text-slate-700 hover:bg-slate-200 disabled:opacity-50"
    >
      {copied ? "Copied" : label}
    </button>
  );
}

function CommandBlock({ command }) {
  return (
    <div className="flex min-w-0 items-start gap-2 rounded-lg bg-slate-950 p-3 text-emerald-200">
      <code className="min-w-0 flex-1 break-all font-mono text-xs leading-5">{command}</code>
      <CopyButton value={command} />
    </div>
  );
}

export default function MountPage() {
  const [info, setInfo] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  useEffect(() => {
    let cancelled = false;
    api("/webdav/info")
      .then((res) => {
        if (cancelled) return;
        if (res.ok) setInfo(res.data);
        else setError(res.error?.message || "Unable to load WebDAV settings");
      })
      .catch((err) => {
        if (!cancelled) setError(err.message || "Unable to load WebDAV settings");
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => { cancelled = true; };
  }, []);

  const curlCommand = useMemo(() => {
    if (!info?.url) return "";
    const auth = info.username ? ` -u ${info.username}:<pool-password>` : "";
    return `curl${auth} -T ./photo.jpg ${info.url}photo.jpg`;
  }, [info]);

  return (
    <div className="space-y-4">
      <PageHeader
        eyebrow="Standard access"
        title="Mount"
        description="Connect this pool from file managers and tools through WebDAV."
      />

      {loading && <EmptyState title="Loading WebDAV settings..." description="Reading the local agent configuration." />}
      {error && <InlineMessage tone="error">{error}</InlineMessage>}

      {info && (
        <>
          <Section title="WebDAV endpoint" description="Use the same pool username and password that you use for this WebUI.">
            <div className="grid gap-3 lg:grid-cols-[1fr_auto] lg:items-center">
              <div className="min-w-0 rounded-lg border border-slate-200 bg-slate-50 p-3">
                <p className="text-xs font-semibold uppercase text-slate-500">Address</p>
                <p className="mt-1 break-all font-mono text-sm text-slate-900">{info.url}</p>
              </div>
              <CopyButton value={info.url} label="Copy address" />
            </div>
            <div className="mt-3 grid gap-3 sm:grid-cols-2">
              <div className="rounded-lg border border-slate-200 bg-white p-3">
                <p className="text-xs font-semibold uppercase text-slate-500">Username</p>
                <p className="mt-1 font-mono text-sm text-slate-900">{info.username || "not required"}</p>
              </div>
              <div className="rounded-lg border border-slate-200 bg-white p-3">
                <p className="text-xs font-semibold uppercase text-slate-500">Authentication</p>
                <p className="mt-1 text-sm font-semibold text-slate-900">{info.auth_required ? "Pool password required" : "Not required"}</p>
              </div>
            </div>
          </Section>

          <div className="grid gap-4 lg:grid-cols-3">
            <Section title="macOS Finder">
              <ol className="space-y-2 text-sm leading-6 text-slate-600">
                <li>Open Finder and choose Go, then Connect to Server.</li>
                <li>Paste the WebDAV address.</li>
                <li>Sign in with the pool username and password.</li>
              </ol>
            </Section>
            <Section title="Windows">
              <ol className="space-y-2 text-sm leading-6 text-slate-600">
                <li>Open This PC and choose Add a network location.</li>
                <li>Paste the WebDAV address when asked for the location.</li>
                <li>Use the pool credentials when Windows prompts for sign-in.</li>
              </ol>
            </Section>
            <Section title="Android">
              <ol className="space-y-2 text-sm leading-6 text-slate-600">
                <li>Open a file manager with WebDAV support.</li>
                <li>Add a WebDAV server using the copied address.</li>
                <li>Use the pool credentials and keep the connection on your LAN.</li>
              </ol>
            </Section>
          </div>

          <Section title="curl smoke test" description="Useful when checking whether the mount endpoint accepts writes.">
            <CommandBlock command={curlCommand} />
          </Section>

          <InlineMessage tone="info">
            WebDAV writes into the pool like normal uploads. It does not manage or delete your source files.
          </InlineMessage>
        </>
      )}
    </div>
  );
}
