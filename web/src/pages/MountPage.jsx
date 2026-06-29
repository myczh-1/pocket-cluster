import { useEffect, useMemo, useState } from "react";
import { api } from "../api";
import { EmptyState, InlineMessage, PageHeader, Section } from "../components/common";

function CopyButton({ value, label = "复制" }) {
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
      {copied ? "已复制" : label}
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
        else setError(res.error?.message || "无法加载 WebDAV 设置");
      })
      .catch((err) => {
        if (!cancelled) setError(err.message || "无法加载 WebDAV 设置");
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
        eyebrow="标准访问"
        title="挂载"
        description="通过 WebDAV 从文件管理器和工具访问当前存储池。"
      />

      {loading && <EmptyState title="WebDAV 设置加载中..." description="正在读取本地 agent 配置。" />}
      {error && <InlineMessage tone="error">{error}</InlineMessage>}

      {info && (
        <>
          <Section title="WebDAV 地址" description="使用与当前 Web UI 相同的存储池用户名和密码。">
            <div className="grid gap-3 lg:grid-cols-[1fr_auto] lg:items-center">
              <div className="min-w-0 rounded-lg border border-slate-200 bg-slate-50 p-3">
                <p className="text-xs font-semibold uppercase text-slate-500">地址</p>
                <p className="mt-1 break-all font-mono text-sm text-slate-900">{info.url}</p>
              </div>
              <CopyButton value={info.url} label="复制地址" />
            </div>
            <div className="mt-3 grid gap-3 sm:grid-cols-2">
              <div className="rounded-lg border border-slate-200 bg-white p-3">
                <p className="text-xs font-semibold uppercase text-slate-500">用户名</p>
                <p className="mt-1 font-mono text-sm text-slate-900">{info.username || "无需用户名"}</p>
              </div>
              <div className="rounded-lg border border-slate-200 bg-white p-3">
                <p className="text-xs font-semibold uppercase text-slate-500">认证</p>
                <p className="mt-1 text-sm font-semibold text-slate-900">{info.auth_required ? "需要存储池密码" : "无需认证"}</p>
              </div>
            </div>
          </Section>

          <div className="grid gap-4 lg:grid-cols-3">
            <Section title="macOS Finder">
              <ol className="space-y-2 text-sm leading-6 text-slate-600">
                <li>打开 Finder，选择“前往”，然后点击“连接服务器”。</li>
                <li>粘贴 WebDAV 地址。</li>
                <li>使用存储池用户名和密码登录。</li>
              </ol>
            </Section>
            <Section title="Windows">
              <ol className="space-y-2 text-sm leading-6 text-slate-600">
                <li>打开“此电脑”，选择“添加网络位置”。</li>
                <li>在要求填写位置时粘贴 WebDAV 地址。</li>
                <li>Windows 弹出登录时，输入存储池凭据。</li>
              </ol>
            </Section>
            <Section title="Android">
              <ol className="space-y-2 text-sm leading-6 text-slate-600">
                <li>打开支持 WebDAV 的文件管理器。</li>
                <li>使用复制好的地址添加 WebDAV 服务器。</li>
                <li>输入存储池凭据，并确保设备仍在局域网内。</li>
              </ol>
            </Section>
          </div>

          <Section title="curl 冒烟测试" description="用于快速确认挂载端点是否接受写入。">
            <CommandBlock command={curlCommand} />
          </Section>

          <InlineMessage tone="info">
            WebDAV 写入会像普通上传一样进入存储池，不会自动管理或删除源文件。
          </InlineMessage>
        </>
      )}
    </div>
  );
}
