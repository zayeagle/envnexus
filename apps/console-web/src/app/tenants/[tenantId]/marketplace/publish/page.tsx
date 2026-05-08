"use client";

import { useCallback, useMemo, useState } from "react";
import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { postFormData, APIError } from "@/lib/api/client";
import { useAuth } from "@/lib/auth/AuthContext";
import { useLanguage } from "@/lib/i18n/LanguageContext";
import { useDict } from "@/lib/i18n/dictionary";

const ITEM_TYPES = ["plugin", "mcp", "skill", "rule", "subagent"] as const;
type ItemType = (typeof ITEM_TYPES)[number];

function isValidJson(s: string): boolean {
  const t = s.trim();
  if (!t) return false;
  try {
    JSON.parse(t);
    return true;
  } catch {
    return false;
  }
}

export default function MarketplacePublishPage() {
  const params = useParams<{ tenantId: string }>();
  const tenantId = params?.tenantId;
  const router = useRouter();
  const { user, loading: authLoading } = useAuth();
  const { lang } = useLanguage();
  const t = useDict("marketplace", lang);
  const ct = useDict("common", lang);

  const isSuper = !!user?.platform_super_admin;

  const typeOptionLabel = (k: ItemType) => {
    if (k === "plugin") return t.typePlugin;
    if (k === "mcp") return t.typeMcp;
    if (k === "skill") return t.typeSkill;
    if (k === "rule") return t.typeRule;
    return t.typeSubagent;
  };

  const [name, setName] = useState("");
  const [type, setType] = useState<ItemType>("skill");
  const [version, setVersion] = useState("1.0.0");
  const [description, setDescription] = useState("");
  const [author, setAuthor] = useState("");
  const [file, setFile] = useState<File | null>(null);
  const [payload, setPayload] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const isPlugin = type === "plugin";
  const isArchiveType = type === "skill" || type === "rule";
  const isJsonType = type === "mcp" || type === "subagent";

  const onFileChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const f = e.target.files?.[0];
    setFile(f ?? null);
  };

  const validate = useCallback((): string | null => {
    if (!name.trim() || !version.trim()) {
      return ct.error;
    }
    if (isPlugin) {
      if (!file) return t.needPluginFile;
      const n = file.name.toLowerCase();
      if (!n.endsWith(".vsix")) return t.needPluginFile;
    } else if (isArchiveType) {
      if (!file) return t.needArchiveFile;
      const n = file.name.toLowerCase();
      if (!n.endsWith(".zip")) return t.needArchiveFile;
    } else {
      if (file) {
        return null;
      }
      if (!isValidJson(payload)) {
        if (!payload.trim()) return t.needFileOrJson;
        return t.invalidJson;
      }
    }
    return null;
  }, [name, version, isPlugin, isArchiveType, file, payload, t, ct.error]);

  const onSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!tenantId) return;
    const v = validate();
    if (v) {
      setError(v);
      return;
    }
    setError(null);
    setSubmitting(true);
    const fd = new FormData();
    fd.append("type", type);
    fd.append("name", name.trim());
    fd.append("version", version.trim());
    fd.append("description", description.trim());
    fd.append("author", author.trim());
    fd.append("status", "published");
    if ((isPlugin || isArchiveType) && file) {
      fd.append("file", file, file.name);
    } else if (isJsonType) {
      if (file) {
        fd.append("file", file, file.name);
      } else {
        fd.append("payload", payload.trim());
      }
    }
    try {
      await postFormData<{ id: string }>(`/tenants/${tenantId}/marketplace/items`, fd);
      router.push(`/tenants/${tenantId}/marketplace`);
    } catch (err) {
      if (err instanceof APIError) {
        setError(err.message);
      } else {
        setError(ct.error);
      }
    } finally {
      setSubmitting(false);
    }
  };

  const backHref = useMemo(
    () => (tenantId ? `/tenants/${tenantId}/marketplace` : "/"),
    [tenantId],
  );

  if (authLoading) {
    return (
      <div className="p-8 text-center text-sm text-slate-500">{ct.loading}</div>
    );
  }

  if (!isSuper) {
    return (
      <div>
        <div className="mb-6">
          <Link
            href={backHref}
            className="text-sm font-medium text-indigo-600 hover:text-indigo-800"
          >
            {t.backToMarketplace}
          </Link>
        </div>
        <p className="text-sm text-slate-600">{t.accessDeniedPublish}</p>
      </div>
    );
  }

  return (
    <div>
      <div className="mb-6">
        <Link
          href={backHref}
          className="text-sm font-medium text-indigo-600 hover:text-indigo-800"
        >
          {t.backToMarketplace}
        </Link>
        <h1 className="mt-2 text-xl font-semibold text-slate-900 tracking-tight">
          {t.publishTitle}
        </h1>
        <p className="mt-1 text-sm text-slate-500">{t.publishSubtitle}</p>
      </div>

      {error && (
        <div className="mb-4 rounded-lg border border-red-100 bg-red-50 px-3 py-2 text-sm text-red-700">
          {error}
        </div>
      )}
      <form
        onSubmit={(e) => void onSubmit(e)}
        className="max-w-xl space-y-4 rounded-xl border border-slate-200/80 bg-white p-6"
      >
        <div>
          <label htmlFor="mp-name" className="block text-sm font-medium text-slate-700">
            {t.nameLabel}
          </label>
          <input
            id="mp-name"
            type="text"
            value={name}
            onChange={(e) => setName(e.target.value)}
            required
            className="mt-1 w-full rounded-lg border border-slate-200 px-3 py-2 text-sm text-slate-900 focus:border-indigo-500 focus:outline-none focus:ring-1 focus:ring-indigo-500"
          />
        </div>

        <div>
          <label htmlFor="mp-type" className="block text-sm font-medium text-slate-700">
            {t.typeLabel}
          </label>
          <select
            id="mp-type"
            value={type}
            onChange={(e) => {
              setType(e.target.value as ItemType);
              setFile(null);
            }}
            className="mt-1 w-full rounded-lg border border-slate-200 px-3 py-2 text-sm text-slate-900 focus:border-indigo-500 focus:outline-none focus:ring-1 focus:ring-indigo-500"
          >
            {ITEM_TYPES.map((k) => (
              <option key={k} value={k}>
                {typeOptionLabel(k)} ({k})
              </option>
            ))}
          </select>
        </div>

        <div>
          <label htmlFor="mp-version" className="block text-sm font-medium text-slate-700">
            {t.versionLabel}
          </label>
          <input
            id="mp-version"
            type="text"
            value={version}
            onChange={(e) => setVersion(e.target.value)}
            required
            className="mt-1 w-full rounded-lg border border-slate-200 px-3 py-2 text-sm text-slate-900 focus:border-indigo-500 focus:outline-none focus:ring-1 focus:ring-indigo-500"
          />
        </div>

        <div>
          <label htmlFor="mp-desc" className="block text-sm font-medium text-slate-700">
            {t.descriptionLabel}
          </label>
          <textarea
            id="mp-desc"
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            rows={3}
            className="mt-1 w-full rounded-lg border border-slate-200 px-3 py-2 text-sm text-slate-900 focus:border-indigo-500 focus:outline-none focus:ring-1 focus:ring-indigo-500"
          />
        </div>

        <div>
          <label htmlFor="mp-author" className="block text-sm font-medium text-slate-700">
            {t.authorLabel}
          </label>
          <input
            id="mp-author"
            type="text"
            value={author}
            onChange={(e) => setAuthor(e.target.value)}
            className="mt-1 w-full rounded-lg border border-slate-200 px-3 py-2 text-sm text-slate-900 focus:border-indigo-500 focus:outline-none focus:ring-1 focus:ring-indigo-500"
          />
        </div>

        {isPlugin ? (
          <div>
            <label htmlFor="mp-file-plugin" className="block text-sm font-medium text-slate-700">
              {t.fileLabel}
            </label>
            <p className="mt-0.5 text-xs text-slate-500">{t.pluginVsixOnly}</p>
            <input
              id="mp-file-plugin"
              type="file"
              accept=".vsix,application/vsix+zip"
              onChange={onFileChange}
              className="mt-1 block w-full text-sm text-slate-600 file:mr-3 file:rounded-md file:border-0 file:bg-indigo-50 file:px-3 file:py-1.5 file:text-sm file:font-medium file:text-indigo-800"
            />
          </div>
        ) : isArchiveType ? (
          <div>
            <label htmlFor="mp-file-archive" className="block text-sm font-medium text-slate-700">
              {t.fileLabel}
            </label>
            <p className="mt-0.5 text-xs text-slate-500">{t.archiveZipOnly}</p>
            <input
              id="mp-file-archive"
              type="file"
              accept=".zip,application/zip"
              onChange={onFileChange}
              className="mt-1 block w-full text-sm text-slate-600 file:mr-3 file:rounded-md file:border-0 file:bg-indigo-50 file:px-3 file:py-1.5 file:text-sm file:font-medium file:text-indigo-800"
            />
          </div>
        ) : (
          <>
            <div>
              <label htmlFor="mp-file" className="block text-sm font-medium text-slate-700">
                {t.fileLabel}
              </label>
              <p className="mt-0.5 text-xs text-slate-500">{t.fileOrJson}</p>
              <input
                id="mp-file"
                type="file"
                accept="application/json,.json"
                onChange={onFileChange}
                className="mt-1 block w-full text-sm text-slate-600 file:mr-3 file:rounded-md file:border-0 file:bg-slate-50 file:px-3 file:py-1.5 file:text-sm file:font-medium file:text-slate-800"
              />
            </div>
            <div>
              <label htmlFor="mp-payload" className="block text-sm font-medium text-slate-700">
                {t.payloadLabel}
              </label>
              <textarea
                id="mp-payload"
                value={payload}
                onChange={(e) => setPayload(e.target.value)}
                rows={8}
                placeholder='{"key": "value"}'
                disabled={!!file}
                className="mt-1 w-full rounded-lg border border-slate-200 px-3 py-2 font-mono text-sm text-slate-900 focus:border-indigo-500 focus:outline-none focus:ring-1 focus:ring-indigo-500 disabled:bg-slate-50 disabled:text-slate-500"
              />
            </div>
          </>
        )}

        <div className="pt-2">
          <button
            type="submit"
            disabled={submitting}
            className="rounded-lg bg-indigo-600 px-4 py-2 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-50"
          >
            {submitting ? ct.loading : t.submitPublish}
          </button>
        </div>
      </form>
    </div>
  );
}
