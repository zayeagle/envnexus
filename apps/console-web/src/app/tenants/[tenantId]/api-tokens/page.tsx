"use client";

import { useCallback, useEffect, useState } from "react";
import { useParams } from "next/navigation";
import { api, APIError } from "@/lib/api/client";
import { useLanguage } from "@/lib/i18n/LanguageContext";
import { useDict } from "@/lib/i18n/dictionary";

interface ApiTokenRow {
  id: string;
  name: string;
  token_prefix: string;
  scopes: string[] | null;
  expires_at: string | null;
  last_used_at: string | null;
  revoked_at: string | null;
  created_at: string;
}

interface CreatedToken {
  id: string;
  name: string;
  token: string;
  token_prefix: string;
  scopes: string[];
  expires_at: string | null;
  created_at: string;
}

const EXPIRY_OPTIONS = [
  { value: 0, labelKey: "neverExpires" as const },
  { value: 2592000, labelKey: "days30" as const },
  { value: 7776000, labelKey: "days90" as const },
  { value: 31536000, labelKey: "days365" as const },
];

export default function ApiTokensPage() {
  const params = useParams<{ tenantId: string }>();
  const tenantId = params?.tenantId;
  const { lang } = useLanguage();
  const t = useDict("apiTokens", lang);
  const ct = useDict("common", lang);

  const [rows, setRows] = useState<ApiTokenRow[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // create modal
  const [showCreate, setShowCreate] = useState(false);
  const [createName, setCreateName] = useState("");
  const [createExpiry, setCreateExpiry] = useState(0);
  const [creating, setCreating] = useState(false);

  // created token display
  const [createdToken, setCreatedToken] = useState<CreatedToken | null>(null);
  const [copied, setCopied] = useState(false);

  // revoke modal
  const [revokeId, setRevokeId] = useState<string | null>(null);
  const [revoking, setRevoking] = useState(false);

  const load = useCallback(async () => {
    if (!tenantId) return;
    setLoading(true);
    setError(null);
    try {
      const data = await api.get<{ items?: ApiTokenRow[] } | ApiTokenRow[]>(
        `/tenants/${tenantId}/api-tokens?page=1&page_size=100`
      );
      if (Array.isArray(data)) {
        setRows(data);
      } else {
        setRows(Array.isArray(data?.items) ? data.items! : []);
      }
    } catch (e) {
      setError(e instanceof APIError ? e.message : ct.error);
      setRows([]);
    } finally {
      setLoading(false);
    }
  }, [tenantId, ct.error]);

  useEffect(() => {
    void load();
  }, [load]);

  const handleCreate = async () => {
    if (!tenantId || !createName.trim()) return;
    setCreating(true);
    setError(null);
    try {
      const body: Record<string, unknown> = { name: createName.trim() };
      if (createExpiry > 0) {
        body.expires_in = createExpiry;
      }
      const data = await api.post<CreatedToken>(
        `/tenants/${tenantId}/api-tokens`,
        body
      );
      setCreatedToken(data);
      setShowCreate(false);
      setCreateName("");
      setCreateExpiry(0);
      await load();
    } catch (e) {
      setError(e instanceof APIError ? e.message : ct.error);
    } finally {
      setCreating(false);
    }
  };

  const handleCopy = (token: string) => {
    if (navigator.clipboard?.writeText) {
      navigator.clipboard.writeText(token).then(() => {
        setCopied(true);
        setTimeout(() => setCopied(false), 2000);
      }).catch(() => fallbackCopy(token));
    } else {
      fallbackCopy(token);
    }
  };

  const fallbackCopy = (text: string) => {
    const textarea = document.createElement("textarea");
    textarea.value = text;
    textarea.style.position = "fixed";
    textarea.style.opacity = "0";
    document.body.appendChild(textarea);
    textarea.select();
    try {
      document.execCommand("copy");
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      // last resort: user must manually copy from the select-all code block
    }
    document.body.removeChild(textarea);
  };

  const confirmRevoke = async () => {
    if (!tenantId || !revokeId) return;
    setRevoking(true);
    setError(null);
    try {
      await api.delete(`/tenants/${tenantId}/api-tokens/${revokeId}`);
      setRevokeId(null);
      await load();
    } catch (e) {
      setError(e instanceof APIError ? e.message : ct.error);
    } finally {
      setRevoking(false);
    }
  };

  const fmt = (iso?: string | null) => {
    if (!iso) return "—";
    try {
      return new Date(iso).toLocaleString(lang === "zh" ? "zh-CN" : "en-US");
    } catch {
      return iso;
    }
  };

  const tokenStatus = (row: ApiTokenRow) => {
    if (row.revoked_at) return { label: t.statusRevoked, cls: "bg-red-50 text-red-700 border-red-100" };
    if (row.expires_at && new Date(row.expires_at) < new Date()) return { label: t.statusExpired, cls: "bg-amber-50 text-amber-700 border-amber-100" };
    return { label: t.statusActive, cls: "bg-emerald-50 text-emerald-700 border-emerald-100" };
  };

  return (
    <div>
      {/* Header */}
      <div className="mb-6 flex items-start justify-between">
        <div>
          <h1 className="text-xl font-semibold text-slate-900 tracking-tight">{t.title}</h1>
          <p className="mt-1 text-sm text-slate-500">{t.subtitle}</p>
        </div>
        <button
          type="button"
          onClick={() => setShowCreate(true)}
          className="rounded-lg bg-indigo-600 px-4 py-2 text-sm font-medium text-white hover:bg-indigo-700 transition-colors"
        >
          {t.createToken}
        </button>
      </div>

      {error && (
        <div className="mb-4 rounded-lg border border-red-100 bg-red-50 px-3 py-2 text-sm text-red-700">{error}</div>
      )}

      {/* Token list */}
      <div className="rounded-xl border border-slate-200/80 bg-white overflow-hidden">
        {loading ? (
          <div className="p-8 text-center text-sm text-slate-500">{ct.loading}</div>
        ) : rows.length === 0 ? (
          <div className="p-8 text-center text-sm text-slate-500">{t.noTokens}</div>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-left text-sm">
              <thead>
                <tr className="border-b border-slate-100 bg-slate-50/80">
                  <th className="px-4 py-3 font-medium text-slate-600">{t.tokenName}</th>
                  <th className="px-4 py-3 font-medium text-slate-600">{t.prefix}</th>
                  <th className="px-4 py-3 font-medium text-slate-600">{t.scopes}</th>
                  <th className="px-4 py-3 font-medium text-slate-600">{t.status}</th>
                  <th className="px-4 py-3 font-medium text-slate-600">{t.expiresIn}</th>
                  <th className="px-4 py-3 font-medium text-slate-600">{t.lastUsed}</th>
                  <th className="px-4 py-3 font-medium text-slate-600">{t.createdAt}</th>
                  <th className="px-4 py-3 font-medium text-slate-600 w-28">{ct.actions}</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((row) => {
                  const st = tokenStatus(row);
                  const isActive = !row.revoked_at && !(row.expires_at && new Date(row.expires_at) < new Date());
                  return (
                    <tr key={row.id} className="border-b border-slate-50 last:border-0">
                      <td className="px-4 py-3 text-slate-800 font-medium">{row.name}</td>
                      <td className="px-4 py-3">
                        <code className="rounded bg-slate-100 px-1.5 py-0.5 text-xs text-slate-600 font-mono">{row.token_prefix}…</code>
                      </td>
                      <td className="px-4 py-3">
                        {(row.scopes || []).map((s) => (
                          <span key={s} className="inline-block rounded-full border border-indigo-100 bg-indigo-50 px-2 py-0.5 text-xs text-indigo-700 mr-1">
                            {s === "read-only" ? t.scopeReadOnly : s}
                          </span>
                        ))}
                      </td>
                      <td className="px-4 py-3">
                        <span className={`inline-block rounded-full border px-2 py-0.5 text-xs font-medium ${st.cls}`}>{st.label}</span>
                      </td>
                      <td className="px-4 py-3 text-slate-600">{row.expires_at ? fmt(row.expires_at) : t.neverExpires}</td>
                      <td className="px-4 py-3 text-slate-600">{row.last_used_at ? fmt(row.last_used_at) : t.neverUsed}</td>
                      <td className="px-4 py-3 text-slate-600">{fmt(row.created_at)}</td>
                      <td className="px-4 py-3">
                        {isActive && (
                          <button
                            type="button"
                            onClick={() => setRevokeId(row.id)}
                            className="text-sm font-medium text-red-600 hover:text-red-800"
                          >
                            {t.revoke}
                          </button>
                        )}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </div>

      {/* Create modal */}
      {showCreate && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/40 p-4">
          <div className="w-full max-w-lg rounded-2xl border border-slate-200 bg-white p-6 shadow-elevated">
            <div className="flex items-center justify-between">
              <h2 className="text-base font-semibold text-slate-900">{t.createToken}</h2>
              <button type="button" onClick={() => setShowCreate(false)} className="text-gray-400 hover:text-gray-600">
                <svg className="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}><path strokeLinecap="round" strokeLinejoin="round" d="M6 18L18 6M6 6l12 12" /></svg>
              </button>
            </div>

            <div className="mt-4 space-y-4">
              <div>
                <label className="block text-sm font-medium text-slate-700 mb-1">{t.tokenName}</label>
                <input
                  type="text"
                  value={createName}
                  onChange={(e) => setCreateName(e.target.value)}
                  placeholder={t.tokenNamePlaceholder}
                  className="w-full rounded-lg border border-slate-200 px-3 py-2 text-sm text-slate-800 placeholder:text-slate-400 focus:border-indigo-300 focus:outline-none focus:ring-2 focus:ring-indigo-100"
                  autoFocus
                />
              </div>

              <div>
                <label className="block text-sm font-medium text-slate-700 mb-1">{t.scopes}</label>
                <div className="flex items-center gap-2">
                  <span className="inline-block rounded-full border border-indigo-100 bg-indigo-50 px-2.5 py-1 text-xs text-indigo-700 font-medium">
                    {t.scopeReadOnly}
                  </span>
                  <span className="text-xs text-slate-400">
                    {lang === "zh" ? "（默认，不可更改）" : "(default, cannot be changed)"}
                  </span>
                </div>
              </div>

              <div>
                <label className="block text-sm font-medium text-slate-700 mb-1">{t.expiresIn}</label>
                <select
                  value={createExpiry}
                  onChange={(e) => setCreateExpiry(Number(e.target.value))}
                  className="w-full rounded-lg border border-slate-200 px-3 py-2 text-sm text-slate-800 focus:border-indigo-300 focus:outline-none focus:ring-2 focus:ring-indigo-100"
                >
                  {EXPIRY_OPTIONS.map((opt) => (
                    <option key={opt.value} value={opt.value}>{t[opt.labelKey]}</option>
                  ))}
                </select>
              </div>
            </div>

            <div className="mt-6 flex justify-end gap-2">
              <button
                type="button"
                onClick={() => setShowCreate(false)}
                className="rounded-lg border border-slate-200 px-4 py-2 text-sm font-medium text-slate-700 hover:bg-slate-50"
              >
                {ct.cancel}
              </button>
              <button
                type="button"
                disabled={creating || !createName.trim()}
                onClick={() => void handleCreate()}
                className="rounded-lg bg-indigo-600 px-4 py-2 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-50"
              >
                {creating ? ct.loading : t.create}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Token created — show once */}
      {createdToken && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/40 p-4">
          <div className="w-full max-w-lg rounded-2xl border border-slate-200 bg-white p-6 shadow-elevated">
            <div className="flex items-center justify-between">
              <h2 className="text-base font-semibold text-emerald-700">{t.tokenCreated}</h2>
              <button type="button" onClick={() => setCreatedToken(null)} className="text-gray-400 hover:text-gray-600">
                <svg className="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}><path strokeLinecap="round" strokeLinejoin="round" d="M6 18L18 6M6 6l12 12" /></svg>
              </button>
            </div>

            <div className="mt-3 rounded-lg border border-amber-200 bg-amber-50 px-3 py-2 text-sm text-amber-800">
              {t.tokenCreatedHint}
            </div>

            <div className="mt-4 flex items-center gap-2">
              <code className="flex-1 rounded-lg border border-slate-200 bg-slate-50 px-3 py-2.5 text-sm font-mono text-slate-800 break-all select-all">
                {createdToken.token}
              </code>
              <button
                type="button"
                onClick={() => void handleCopy(createdToken.token)}
                className="flex-shrink-0 rounded-lg border border-slate-200 px-3 py-2.5 text-sm font-medium text-slate-700 hover:bg-slate-50"
              >
                {copied ? t.copied : t.copyToken}
              </button>
            </div>

            <div className="mt-4 flex justify-end">
              <button
                type="button"
                onClick={() => setCreatedToken(null)}
                className="rounded-lg bg-indigo-600 px-4 py-2 text-sm font-medium text-white hover:bg-indigo-700"
              >
                {ct.confirm}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Revoke confirmation */}
      {revokeId != null && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/40 p-4">
          <div className="w-full max-w-md rounded-2xl border border-slate-200 bg-white p-6 shadow-elevated">
            <div className="flex items-center justify-between">
              <h2 className="text-base font-semibold text-slate-900">{t.revokeTitle}</h2>
              <button type="button" onClick={() => setRevokeId(null)} className="text-gray-400 hover:text-gray-600">
                <svg className="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}><path strokeLinecap="round" strokeLinejoin="round" d="M6 18L18 6M6 6l12 12" /></svg>
              </button>
            </div>
            <p className="mt-2 text-sm text-slate-600">{t.revokeBody}</p>
            <div className="mt-6 flex justify-end gap-2">
              <button
                type="button"
                onClick={() => setRevokeId(null)}
                className="rounded-lg border border-slate-200 px-4 py-2 text-sm font-medium text-slate-700 hover:bg-slate-50"
              >
                {ct.cancel}
              </button>
              <button
                type="button"
                disabled={revoking}
                onClick={() => void confirmRevoke()}
                className="rounded-lg bg-red-600 px-4 py-2 text-sm font-medium text-white hover:bg-red-700"
              >
                {revoking ? ct.loading : ct.confirm}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
