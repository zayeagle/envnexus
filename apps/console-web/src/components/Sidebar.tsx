"use client";

import { useLayoutEffect, useMemo, useState, type ReactNode } from 'react';
import Link from 'next/link';
import { useRouter, usePathname } from 'next/navigation';
import { useLanguage } from '@/lib/i18n/LanguageContext';
import { useAuth } from '@/lib/auth/AuthContext';
import { useDict } from '@/lib/i18n/dictionary';

function Icon({ name, className = "w-4 h-4" }: { name: string; className?: string }) {
  const icons: Record<string, JSX.Element> = {
    dashboard: <svg className={className} fill="none" viewBox="0 0 24 24" strokeWidth={1.5} stroke="currentColor"><path strokeLinecap="round" strokeLinejoin="round" d="M3.75 6A2.25 2.25 0 0 1 6 3.75h2.25A2.25 2.25 0 0 1 10.5 6v2.25a2.25 2.25 0 0 1-2.25 2.25H6a2.25 2.25 0 0 1-2.25-2.25V6ZM3.75 15.75A2.25 2.25 0 0 1 6 13.5h2.25a2.25 2.25 0 0 1 2.25 2.25V18a2.25 2.25 0 0 1-2.25 2.25H6A2.25 2.25 0 0 1 3.75 18v-2.25ZM13.5 6a2.25 2.25 0 0 1 2.25-2.25H18A2.25 2.25 0 0 1 20.25 6v2.25A2.25 2.25 0 0 1 18 10.5h-2.25a2.25 2.25 0 0 1-2.25-2.25V6ZM13.5 15.75a2.25 2.25 0 0 1 2.25-2.25H18a2.25 2.25 0 0 1 2.25 2.25V18A2.25 2.25 0 0 1 18 20.25h-2.25A2.25 2.25 0 0 1 13.5 18v-2.25Z" /></svg>,
    tenants: <svg className={className} fill="none" viewBox="0 0 24 24" strokeWidth={1.5} stroke="currentColor"><path strokeLinecap="round" strokeLinejoin="round" d="M2.25 21h19.5m-18-18v18m10.5-18v18m6-13.5V21M6.75 6.75h.75m-.75 3h.75m-.75 3h.75m3-6h.75m-.75 3h.75m-.75 3h.75M6.75 21v-3.375c0-.621.504-1.125 1.125-1.125h2.25c.621 0 1.125.504 1.125 1.125V21M3 3h12m-.75 4.5H21m-3.75 3h.008v.008h-.008v-.008Zm0 3h.008v.008h-.008v-.008Zm0 3h.008v.008h-.008v-.008Z" /></svg>,
    devices: <svg className={className} fill="none" viewBox="0 0 24 24" strokeWidth={1.5} stroke="currentColor"><path strokeLinecap="round" strokeLinejoin="round" d="M9 17.25v1.007a3 3 0 0 1-.879 2.122L7.5 21h9l-.621-.621A3 3 0 0 1 15 18.257V17.25m6-12V15a2.25 2.25 0 0 1-2.25 2.25H5.25A2.25 2.25 0 0 1 3 15V5.25A2.25 2.25 0 0 1 5.25 3h13.5A2.25 2.25 0 0 1 21 5.25Z" /></svg>,
    sessions: <svg className={className} fill="none" viewBox="0 0 24 24" strokeWidth={1.5} stroke="currentColor"><path strokeLinecap="round" strokeLinejoin="round" d="M7.5 8.25h9m-9 3H12m-9.75 1.51c0 1.6 1.123 2.994 2.707 3.227 1.129.166 2.27.293 3.423.379.35.026.67.21.865.501L12 21l2.755-4.133a1.14 1.14 0 0 1 .865-.501 48.172 48.172 0 0 0 3.423-.379c1.584-.233 2.707-1.626 2.707-3.228V6.741c0-1.602-1.123-2.995-2.707-3.228A48.394 48.394 0 0 0 12 3c-2.392 0-4.744.175-7.043.513C3.373 3.746 2.25 5.14 2.25 6.741v6.018Z" /></svg>,
    command: <svg className={className} fill="none" viewBox="0 0 24 24" strokeWidth={1.5} stroke="currentColor"><path strokeLinecap="round" strokeLinejoin="round" d="m6.75 7.5 3 2.25-3 2.25m4.5 0h3m-9 8.25h13.5A2.25 2.25 0 0 0 21 18V6a2.25 2.25 0 0 0-2.25-2.25H5.25A2.25 2.25 0 0 0 3 6v12a2.25 2.25 0 0 0 2.25 2.25Z" /></svg>,
    approval: <svg className={className} fill="none" viewBox="0 0 24 24" strokeWidth={1.5} stroke="currentColor"><path strokeLinecap="round" strokeLinejoin="round" d="M9 12.75 11.25 15 15 9.75M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0Z" /></svg>,
    audit: <svg className={className} fill="none" viewBox="0 0 24 24" strokeWidth={1.5} stroke="currentColor"><path strokeLinecap="round" strokeLinejoin="round" d="M9 12h3.75M9 15h3.75M9 18h3.75m3 .75H18a2.25 2.25 0 0 0 2.25-2.25V6.108c0-1.135-.845-2.098-1.976-2.192a48.424 48.424 0 0 0-1.123-.08m-5.801 0c-.065.21-.1.433-.1.664 0 .414.336.75.75.75h4.5a.75.75 0 0 0 .75-.75 2.25 2.25 0 0 0-.1-.664m-5.8 0A2.251 2.251 0 0 1 13.5 2.25H15c1.012 0 1.867.668 2.15 1.586m-5.8 0c-.376.023-.75.05-1.124.08C9.095 4.01 8.25 4.973 8.25 6.108V8.25m0 0H4.875c-.621 0-1.125.504-1.125 1.125v11.25c0 .621.504 1.125 1.125 1.125h9.75c.621 0 1.125-.504 1.125-1.125V9.375c0-.621-.504-1.125-1.125-1.125H8.25Z" /></svg>,
    files: <svg className={className} fill="none" viewBox="0 0 24 24" strokeWidth={1.5} stroke="currentColor"><path strokeLinecap="round" strokeLinejoin="round" d="M2.25 12.75V12A2.25 2.25 0 0 1 4.5 9.75h15A2.25 2.25 0 0 1 21.75 12v.75m-8.69-6.44-2.12-2.12a1.5 1.5 0 0 0-1.061-.44H4.5A2.25 2.25 0 0 0 2.25 6v12a2.25 2.25 0 0 0 2.25 2.25h15A2.25 2.25 0 0 0 21.75 18V9a2.25 2.25 0 0 0-2.25-2.25h-5.379a1.5 1.5 0 0 1-1.06-.44Z" /></svg>,
    download: <svg className={className} fill="none" viewBox="0 0 24 24" strokeWidth={1.5} stroke="currentColor"><path strokeLinecap="round" strokeLinejoin="round" d="M3 16.5v2.25A2.25 2.25 0 0 0 5.25 21h13.5A2.25 2.25 0 0 0 21 18.75V16.5M16.5 12 12 16.5m0 0L7.5 12m4.5 4.5V3" /></svg>,
    model: <svg className={className} fill="none" viewBox="0 0 24 24" strokeWidth={1.5} stroke="currentColor"><path strokeLinecap="round" strokeLinejoin="round" d="M9.813 15.904 9 18.75l-.813-2.846a4.5 4.5 0 0 0-3.09-3.09L2.25 12l2.846-.813a4.5 4.5 0 0 0 3.09-3.09L9 5.25l.813 2.846a4.5 4.5 0 0 0 3.09 3.09L15.75 12l-2.846.813a4.5 4.5 0 0 0-3.09 3.09ZM18.259 8.715 18 9.75l-.259-1.035a3.375 3.375 0 0 0-2.455-2.456L14.25 6l1.036-.259a3.375 3.375 0 0 0 2.455-2.456L18 2.25l.259 1.035a3.375 3.375 0 0 0 2.455 2.456L21.75 6l-1.036.259a3.375 3.375 0 0 0-2.455 2.456ZM16.894 20.567 16.5 21.75l-.394-1.183a2.25 2.25 0 0 0-1.423-1.423L13.5 18.75l1.183-.394a2.25 2.25 0 0 0 1.423-1.423l.394-1.183.394 1.183a2.25 2.25 0 0 0 1.423 1.423l1.183.394-1.183.394a2.25 2.25 0 0 0-1.423 1.423Z" /></svg>,
    policy: <svg className={className} fill="none" viewBox="0 0 24 24" strokeWidth={1.5} stroke="currentColor"><path strokeLinecap="round" strokeLinejoin="round" d="M9 12.75 11.25 15 15 9.75m-3-7.036A11.959 11.959 0 0 1 3.598 6 11.99 11.99 0 0 0 3 9.749c0 5.592 3.824 10.29 9 11.623 5.176-1.332 9-6.03 9-11.622 0-1.31-.21-2.571-.598-3.751h-.152c-3.196 0-6.1-1.248-8.25-3.285Z" /></svg>,
    agent: <svg className={className} fill="none" viewBox="0 0 24 24" strokeWidth={1.5} stroke="currentColor"><path strokeLinecap="round" strokeLinejoin="round" d="M15.75 6a3.75 3.75 0 1 1-7.5 0 3.75 3.75 0 0 1 7.5 0ZM4.501 20.118a7.5 7.5 0 0 1 14.998 0A17.933 17.933 0 0 1 12 21.75c-2.676 0-5.216-.584-7.499-1.632Z" /></svg>,
    users: <svg className={className} fill="none" viewBox="0 0 24 24" strokeWidth={1.5} stroke="currentColor"><path strokeLinecap="round" strokeLinejoin="round" d="M15 19.128a9.38 9.38 0 0 0 2.625.372 9.337 9.337 0 0 0 4.121-.952 4.125 4.125 0 0 0-7.533-2.493M15 19.128v-.003c0-1.113-.285-2.16-.786-3.07M15 19.128v.106A12.318 12.318 0 0 1 8.624 21c-2.331 0-4.512-.645-6.374-1.766v-.109a6.375 6.375 0 0 1 11.964-3.07M12 6.375a3.375 3.375 0 1 1-6.75 0 3.375 3.375 0 0 1 6.75 0Zm8.25 2.25a2.625 2.625 0 1 1-5.25 0 2.625 2.625 0 0 1 5.25 0Z" /></svg>,
    roles: <svg className={className} fill="none" viewBox="0 0 24 24" strokeWidth={1.5} stroke="currentColor"><path strokeLinecap="round" strokeLinejoin="round" d="M16.5 10.5V6.75a4.5 4.5 0 1 0-9 0v3.75m-2.25 0h13.5A2.25 2.25 0 0 1 21 12.75v6A2.25 2.25 0 0 1 18.75 21H5.25A2.25 2.25 0 0 1 3 18.75v-6A2.25 2.25 0 0 1 5.25 10.5Z" /></svg>,
    governance: <svg className={className} fill="none" viewBox="0 0 24 24" strokeWidth={1.5} stroke="currentColor"><path strokeLinecap="round" strokeLinejoin="round" d="M10.5 6h9.75M10.5 6a1.5 1.5 0 1 1-3 0m3 0a1.5 1.5 0 1 0-3 0M3.75 6H7.5m3 12h9.75m-9.75 0a1.5 1.5 0 0 1-3 0m3 0a1.5 1.5 0 0 0-3 0m-3.75 0H7.5m9-6h3.75m-3.75 0a1.5 1.5 0 0 1-3 0m3 0a1.5 1.5 0 0 0-3 0m-9.75 0h9.75" /></svg>,
    settings: <svg className={className} fill="none" viewBox="0 0 24 24" strokeWidth={1.5} stroke="currentColor"><path strokeLinecap="round" strokeLinejoin="round" d="M9.594 3.94c.09-.542.56-.94 1.11-.94h2.593c.55 0 1.02.398 1.11.94l.213 1.281c.063.374.313.686.645.87.074.04.147.083.22.127.325.196.72.257 1.075.124l1.217-.456a1.125 1.125 0 0 1 1.37.49l1.296 2.247a1.125 1.125 0 0 1-.26 1.431l-1.003.827c-.293.241-.438.613-.43.992a7.723 7.723 0 0 1 0 .255c-.008.378.137.75.43.991l1.004.827c.424.35.534.955.26 1.43l-1.298 2.247a1.125 1.125 0 0 1-1.369.491l-1.217-.456c-.355-.133-.75-.072-1.076.124a6.47 6.47 0 0 1-.22.128c-.331.183-.581.495-.644.869l-.213 1.281c-.09.543-.56.94-1.11.94h-2.594c-.55 0-1.019-.398-1.11-.94l-.213-1.281c-.062-.374-.312-.686-.644-.87a6.52 6.52 0 0 1-.22-.127c-.325-.196-.72-.257-1.076-.124l-1.217.456a1.125 1.125 0 0 1-1.369-.49l-1.297-2.247a1.125 1.125 0 0 1 .26-1.431l1.004-.827c.292-.24.437-.613.43-.991a6.932 6.932 0 0 1 0-.255c.007-.38-.138-.751-.43-.992l-1.004-.827a1.125 1.125 0 0 1-.26-1.43l1.297-2.247a1.125 1.125 0 0 1 1.37-.491l1.216.456c.356.133.751.072 1.076-.124.072-.044.146-.086.22-.128.332-.183.582-.495.644-.869l.214-1.28Z" /><path strokeLinecap="round" strokeLinejoin="round" d="M15 12a3 3 0 1 1-6 0 3 3 0 0 1 6 0Z" /></svg>,
    chevronDown: <svg className={className} fill="none" viewBox="0 0 24 24" strokeWidth={2} stroke="currentColor"><path strokeLinecap="round" strokeLinejoin="round" d="m19.5 8.25-7.5 7.5-7.5-7.5" /></svg>,
    switchArrows: <svg className={className} fill="none" viewBox="0 0 24 24" strokeWidth={1.5} stroke="currentColor"><path strokeLinecap="round" strokeLinejoin="round" d="M7.5 21 3 16.5m0 0L7.5 12M3 16.5h13.5m0-13.5L21 7.5m0 0L16.5 12M21 7.5H7.5" /></svg>,
    code: <svg className={className} fill="none" viewBox="0 0 24 24" strokeWidth={1.5} stroke="currentColor"><path strokeLinecap="round" strokeLinejoin="round" d="M14.25 9.75 16.5 12l-2.25 2.25m-5.5-4.5L5.25 12l2.25 2.25" /><path strokeLinecap="round" strokeLinejoin="round" d="M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0Z" /></svg>,
  };
  return icons[name] || null;
}

function NavItem({ href, icon, label, active }: { href: string; icon: string; label: string; active: boolean }) {
  return (
    <Link
      href={href}
      className={`flex items-center gap-3 px-3 py-2 rounded-lg text-[13px] font-medium transition-all duration-150 ${
        active
          ? 'bg-indigo-50 text-indigo-700'
          : 'text-slate-600 hover:bg-slate-100 hover:text-slate-900'
      }`}
    >
      <span className={active ? 'text-indigo-500' : 'text-slate-400'}><Icon name={icon} /></span>
      {label}
    </Link>
  );
}

type TenantNavLink = { href: string; icon: string; label: string };
type TenantNavGroup = { id: string; label: string; sectionIcon: string; links: TenantNavLink[] };

function NavSection({
  label,
  sectionIcon,
  open,
  activeChild,
  onToggle,
  children,
}: {
  label: string;
  sectionIcon: string;
  open: boolean;
  activeChild: boolean;
  onToggle: () => void;
  children: ReactNode;
}) {
  const rowActive = activeChild;
  return (
    <div>
      <button
        type="button"
        onClick={onToggle}
        className={`w-full flex items-center justify-between gap-2 px-3 py-2 rounded-lg text-[13px] font-medium transition-all duration-150 ${
          rowActive
            ? 'bg-indigo-50 text-indigo-700 hover:bg-indigo-50'
            : 'text-slate-600 hover:bg-slate-100 hover:text-slate-900'
        }`}
      >
        <span className="flex items-center gap-3 min-w-0 flex-1">
          <span className={`flex-shrink-0 ${rowActive ? 'text-indigo-500' : 'text-slate-400'}`}>
            <Icon name={sectionIcon} />
          </span>
          <span className="truncate text-left">{label}</span>
        </span>
        <span
          className={`flex-shrink-0 transition-transform duration-150 ${open ? 'rotate-180' : ''} ${
            rowActive ? 'text-indigo-500' : 'text-slate-400'
          }`}
        >
          <Icon name="chevronDown" className="w-4 h-4" />
        </span>
      </button>
      {open ? <div className="ml-4 space-y-0.5">{children}</div> : null}
    </div>
  );
}

export default function Sidebar() {
  const { lang } = useLanguage();
  const { activeTenantId, activeTenantName, tenants, switchTenant } = useAuth();
  const router = useRouter();
  const pathname = usePathname();
  const [tenantOpen, setTenantOpen] = useState(false);
  const [tenantSectionOpen, setTenantSectionOpen] = useState<Record<string, boolean>>({});

  const t = useDict('nav', lang);
  const tid = activeTenantId;

  const handleTenantSelect = (newTid: string) => {
    switchTenant(newTid);
    setTenantOpen(false);
    router.push(`/tenants/${newTid}/health`);
  };

  const isActive = (path: string) => pathname === path || pathname.startsWith(path + '/');

  const tenantNavGroups: TenantNavGroup[] = useMemo(() => {
    if (!tid) return [];
    return [
      {
        id: 'ops',
        label: t.navGroupOperations,
        sectionIcon: 'devices',
        links: [
          { href: `/tenants/${tid}/health`, icon: 'dashboard', label: t.healthDashboard },
          { href: `/tenants/${tid}/sessions`, icon: 'sessions', label: t.sessions },
          { href: `/tenants/${tid}/command-tasks`, icon: 'command', label: t.commandTasks },
          { href: `/tenants/${tid}/file-browser`, icon: 'files', label: t.fileBrowser },
          { href: `/tenants/${tid}/device-groups`, icon: 'devices', label: t.deviceGroups },
          { href: `/tenants/${tid}/batch-tasks`, icon: 'command', label: t.batchTasks },
          { href: `/tenants/${tid}/pending-approvals`, icon: 'approval', label: t.pendingApprovals },
          { href: `/tenants/${tid}/approval-policies`, icon: 'policy', label: t.approvalPolicies },
        ],
      },
      {
        id: 'cfg',
        label: t.navGroupConfiguration,
        sectionIcon: 'policy',
        links: [
          { href: `/tenants/${tid}/model-profiles`, icon: 'model', label: t.modelProfiles },
          { href: `/tenants/${tid}/policy-profiles`, icon: 'policy', label: t.policyProfiles },
          { href: `/tenants/${tid}/agent-profiles`, icon: 'agent', label: t.agentProfiles },
          { href: `/tenants/${tid}/download-packages`, icon: 'download', label: t.downloadPackages },
          { href: `/tenants/${tid}/marketplace`, icon: 'model', label: t.marketplace },
          { href: `/tenants/${tid}/developer-settings`, icon: 'code', label: t.developerSettings },
          { href: `/tenants/${tid}/api-tokens`, icon: 'roles', label: t.apiTokens },
        ],
      },
      {
        id: 'org',
        label: t.navGroupOrganization,
        sectionIcon: 'users',
        links: [
          { href: `/tenants/${tid}/users`, icon: 'users', label: t.users },
          { href: `/tenants/${tid}/roles`, icon: 'roles', label: t.roles },
        ],
      },
      {
        id: 'gov',
        label: t.navGroupGovernance,
        sectionIcon: 'governance',
        links: [
          { href: `/tenants/${tid}/governance`, icon: 'governance', label: t.governance },
          { href: `/tenants/${tid}/governance-rules`, icon: 'policy', label: t.governanceRules },
          { href: `/tenants/${tid}/tool-permissions`, icon: 'roles', label: t.toolPermissions },
          { href: `/tenants/${tid}/audit-events`, icon: 'audit', label: t.auditEvents },
        ],
      },
    ];
  }, [tid, t]);

  useLayoutEffect(() => {
    if (!tid || tenantNavGroups.length === 0) return;
    const pathActive = (path: string) => pathname === path || pathname.startsWith(path + '/');
    setTenantSectionOpen((prev) => {
      const next = { ...prev };
      for (const g of tenantNavGroups) {
        if (g.links.some((l) => pathActive(l.href))) {
          next[g.id] = true;
        }
      }
      return next;
    });
  }, [pathname, tid, tenantNavGroups]);

  return (
    <aside className="w-[260px] bg-white border-r border-slate-200/80 flex flex-col h-full">
      {/* Logo */}
      <div className="h-16 flex items-center px-5 border-b border-slate-100">
        <div className="flex items-center gap-2.5">
          <div className="w-8 h-8 rounded-lg bg-gradient-to-br from-indigo-500 to-indigo-600 flex items-center justify-center shadow-sm">
            <span className="text-white text-sm font-bold">E</span>
          </div>
          <span className="text-[15px] font-semibold text-slate-900 tracking-tight">EnvNexus</span>
        </div>
      </div>

      {/* Tenant Switcher */}
      <div className="px-3 pt-2 pb-1.5">
        <div className="relative">
          <button
            onClick={() => setTenantOpen(!tenantOpen)}
            className="w-full flex items-center justify-between gap-2 px-3 py-2.5 rounded-lg border border-slate-200 bg-slate-50/50 hover:bg-slate-100 transition-all duration-150 group"
          >
            <div className="flex items-center gap-2.5 min-w-0">
              <div className="w-6 h-6 rounded-md bg-indigo-100 flex items-center justify-center flex-shrink-0">
                <span className="text-indigo-600 text-[10px] font-bold">{activeTenantName?.charAt(0)?.toUpperCase() || 'T'}</span>
              </div>
              <div className="min-w-0 text-left">
                <div className="text-[13px] font-medium text-slate-800 truncate">{activeTenantName || t.selectTenant}</div>
              </div>
            </div>
            <Icon name="switchArrows" className="w-3.5 h-3.5 text-slate-400 group-hover:text-slate-600 flex-shrink-0" />
          </button>

          {tenantOpen && (
            <>
              <div className="fixed inset-0 z-40" onClick={() => setTenantOpen(false)} />
              <div className="absolute left-0 right-0 top-full mt-1 z-50 bg-white rounded-xl border border-slate-200 shadow-elevated py-1.5 max-h-64 overflow-y-auto">
                <div className="px-3 py-1.5">
                  <span className="text-[10px] font-semibold text-slate-400 uppercase tracking-wider">{t.currentTenant || 'Tenants'}</span>
                </div>
                {tenants.map(tenant => (
                  <button
                    key={tenant.id}
                    onClick={() => handleTenantSelect(tenant.id)}
                    className={`w-full flex items-center gap-2.5 px-3 py-2 text-left hover:bg-slate-50 transition-colors ${
                      tenant.id === tid ? 'bg-indigo-50' : ''
                    }`}
                  >
                    <div className={`w-6 h-6 rounded-md flex items-center justify-center flex-shrink-0 ${
                      tenant.id === tid ? 'bg-indigo-100' : 'bg-slate-100'
                    }`}>
                      <span className={`text-[10px] font-bold ${
                        tenant.id === tid ? 'text-indigo-600' : 'text-slate-500'
                      }`}>{tenant.name.charAt(0).toUpperCase()}</span>
                    </div>
                    <div className="min-w-0">
                      <div className={`text-[13px] font-medium truncate ${
                        tenant.id === tid ? 'text-indigo-700' : 'text-slate-700'
                      }`}>{tenant.name}</div>
                      <div className="text-[11px] text-slate-400 truncate">{tenant.slug}</div>
                    </div>
                    {tenant.id === tid && (
                      <div className="ml-auto flex-shrink-0">
                        <div className="w-1.5 h-1.5 rounded-full bg-indigo-500" />
                      </div>
                    )}
                  </button>
                ))}
              </div>
            </>
          )}
        </div>
      </div>

      {/* Navigation — single stack avoids large sectional gaps (formerly mt-5) */}
      <nav className="flex-1 overflow-y-auto px-3 py-2">
        <div className="space-y-0.5">
          <NavItem href="/overview" icon="dashboard" label={t.dashboard} active={isActive('/overview')} />
          <NavItem href="/tenants" icon="tenants" label={t.allTenants} active={pathname === '/tenants'} />
          {tid &&
            tenantNavGroups.map((group) => (
              <NavSection
                key={group.id}
                label={group.label}
                sectionIcon={group.sectionIcon}
                activeChild={group.links.some((l) => isActive(l.href))}
                open={tenantSectionOpen[group.id] === true}
                onToggle={() =>
                  setTenantSectionOpen((s) => ({ ...s, [group.id]: !s[group.id] }))
                }
              >
                {group.links.map((link) => (
                  <NavItem
                    key={link.href}
                    href={link.href}
                    icon={link.icon}
                    label={link.label}
                    active={isActive(link.href)}
                  />
                ))}
              </NavSection>
            ))}
          <NavItem href="/settings" icon="settings" label={t.settings} active={isActive('/settings')} />
        </div>
      </nav>
    </aside>
  );
}
