import { Copy, Inbox, Loader2, Mail, Plus, RefreshCw, Search } from "lucide-react";
import { FormEvent, useEffect, useMemo, useState } from "react";

type Message = {
  id: string;
  alias: string;
  recipient?: string;
  sender?: string;
  subject?: string;
  code?: string;
  content?: string;
  created_at: string;
};

type InboxResponse = {
  id: string;
  alias: string;
  label?: string;
  email: string;
  created_at: string;
  messages: Message[];
};

type AppConfig = {
  mail_domain: string;
};

const API_URL = import.meta.env.VITE_API_URL || window.location.origin;

export default function App() {
  const [aliasInput, setAliasInput] = useState("");
  const [activeAlias, setActiveAlias] = useState("");
  const [inbox, setInbox] = useState<InboxResponse | null>(null);
  const [mailDomain, setMailDomain] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  const [copied, setCopied] = useState(false);

  const latest = inbox?.messages[0];
  const isDefaultDomain = mailDomain === "mailotp.com";
  const address = inbox?.email || (activeAlias && mailDomain ? `${activeAlias}@${mailDomain}` : "");

  const formattedTime = useMemo(() => {
    if (!latest?.created_at) {
      return "";
    }
    return new Intl.DateTimeFormat(undefined, {
      dateStyle: "medium",
      timeStyle: "medium",
    }).format(new Date(latest.created_at));
  }, [latest?.created_at]);

  useEffect(() => {
    void loadConfig();
  }, []);

  useEffect(() => {
    if (!activeAlias) {
      return;
    }
    const timer = window.setInterval(() => {
      void loadInbox(activeAlias, { quiet: true });
    }, 5000);
    return () => window.clearInterval(timer);
  }, [activeAlias]);

  async function loadConfig() {
    try {
      const response = await fetch(`${API_URL}/api/config`);
      const payload = await parseResponse<AppConfig>(response);
      setMailDomain(payload.mail_domain);
    } catch (err) {
      setError(errorMessage(err));
    }
  }

  async function createInbox(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setLoading(true);
    setError("");
    try {
      const alias = aliasInput.trim();
      const response = await fetch(`${API_URL}/api/inboxes`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(alias ? { alias } : {}),
      });
      const payload = await parseResponse<InboxResponse>(response);
      setInbox(payload);
      setActiveAlias(payload.alias);
      setAliasInput(payload.alias);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setLoading(false);
    }
  }

  async function openInbox(event?: FormEvent<HTMLFormElement>) {
    event?.preventDefault();
    const alias = aliasInput.trim();
    if (!alias) {
      setError("请输入要打开的邮箱别名。");
      return;
    }
    await loadInbox(alias);
  }

  async function loadInbox(alias: string, options?: { quiet?: boolean }) {
    if (!options?.quiet) {
      setLoading(true);
    }
    setError("");
    try {
      const response = await fetch(`${API_URL}/api/inboxes/${encodeURIComponent(alias)}`);
      const payload = await parseResponse<InboxResponse>(response);
      setInbox(payload);
      setActiveAlias(payload.alias);
      setAliasInput(payload.alias);
    } catch (err) {
      if (!options?.quiet) {
        setError(errorMessage(err));
      }
    } finally {
      if (!options?.quiet) {
        setLoading(false);
      }
    }
  }

  async function copyAddress() {
    if (!address) {
      return;
    }
    await navigator.clipboard.writeText(address);
    setCopied(true);
    window.setTimeout(() => setCopied(false), 1200);
  }

  return (
    <main className="shell">
      <section className="header-band">
        <div>
          <p className="eyebrow">MailOTP</p>
          <h1>验证码收件台</h1>
        </div>
        <div className="status-pill">
          <span />
          接口 {API_URL.replace(/^https?:\/\//, "")}
        </div>
      </section>

      <section className="workspace">
        <aside className="sidebar">
          <div className="section-title">
            <Inbox size={18} />
            <span>收件箱</span>
          </div>

          <form className="control-stack" onSubmit={createInbox}>
            <label htmlFor="alias">邮箱别名</label>
            <input
              id="alias"
              value={aliasInput}
              onChange={(event) => setAliasInput(event.target.value)}
              placeholder="u7f3k"
              spellCheck={false}
            />
            <div className="button-row">
              <button type="submit" disabled={loading}>
                {loading ? <Loader2 className="spin" size={16} /> : <Plus size={16} />}
                创建
              </button>
              <button type="button" className="secondary" onClick={() => openInbox()} disabled={loading || !aliasInput.trim()}>
                <Search size={16} />
                打开
              </button>
            </div>
          </form>

          {error ? <div className="error">{error}</div> : null}

          <div className="meta-list">
            <div>
              <span>当前别名</span>
              <strong>{activeAlias || "无"}</strong>
            </div>
            <div>
              <span>刷新频率</span>
              <strong>5 秒</strong>
            </div>
            <div>
              <span>收件域名</span>
              <strong>{mailDomain || "读取中"}</strong>
            </div>
          </div>
        </aside>

        <section className="content">
          {isDefaultDomain ? (
            <div className="warning-bar">
              当前仍使用默认域名 mailotp.com，尚未接入你的 Cloudflare 真实域名。
            </div>
          ) : null}

          <div className="address-bar">
            <div className="address-main">
              <Mail size={18} />
              <span>{address || "创建或打开一个收件箱"}</span>
            </div>
            <div className="address-actions">
              <button className="icon-button" onClick={() => loadInbox(activeAlias)} disabled={!activeAlias || loading} aria-label="刷新收件箱" title="刷新收件箱">
                <RefreshCw size={17} />
              </button>
              <button className="icon-button" onClick={copyAddress} disabled={!address} aria-label="复制邮箱地址" title="复制邮箱地址">
                <Copy size={17} />
              </button>
            </div>
          </div>

          <div className="code-panel">
            <span>最新验证码</span>
            <strong>{latest?.code || "等待中"}</strong>
            <small>{formattedTime || "暂无邮件"}</small>
          </div>

          <div className="messages">
            <div className="section-title compact">
              <Mail size={17} />
              <span>最近邮件</span>
            </div>
            {inbox?.messages.length ? (
              <div className="message-list">
                {inbox.messages.map((message) => (
                  <article className="message-card" key={message.id}>
                    <div className="message-top">
                      <strong>{message.code || "无验证码"}</strong>
                      <time>{new Intl.DateTimeFormat(undefined, { timeStyle: "medium" }).format(new Date(message.created_at))}</time>
                    </div>
                    <p>{message.subject || "无主题"}</p>
                    <span>{message.sender || "未知发件人"}</span>
                  </article>
                ))}
              </div>
            ) : (
              <div className="empty-state">{copied ? "邮箱地址已复制" : "暂无邮件"}</div>
            )}
          </div>
        </section>
      </section>
    </main>
  );
}

async function parseResponse<T>(response: Response): Promise<T> {
  const payload = await response.json().catch(() => ({}));
  if (!response.ok) {
    throw new Error(payload.error || `Request failed with ${response.status}`);
  }
  return payload as T;
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : "发生未知错误";
}
