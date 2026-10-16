'use client';
import Link from 'next/link';
import { usePathname, useRouter } from 'next/navigation';
import { useEffect, useState } from 'react';
import type { Me } from '@pulse/api-client';
import { api, currentContext, handleAuthError, selectOrg, selectProject, storedOrg, storedProject } from '@/lib/api';
import { cn } from '@/lib/cn';

const links = [
  { href: '/dashboards', label: 'Dashboards', icon: 'M3 3h8v8H3zM13 3h8v5h-8zM13 10h8v11h-8zM3 13h8v8H3z' },
  { href: '/insights', label: 'Insights', icon: 'M4 19V9m6 10V5m6 14v-7m4 7H2' },
  { href: '/retention', label: 'Retention', icon: 'M3 4h18v16H3zM3 10h18M9 4v16' },
  { href: '/paths', label: 'Paths', icon: 'M5 6h4l6 12h4M5 18h4l2-4M15 6h4' },
  { href: '/persons', label: 'Persons', icon: 'M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2M9 11a4 4 0 1 0 0-8 4 4 0 0 0 0 8ZM22 21v-2a4 4 0 0 0-3-3.9M16 3.1a4 4 0 0 1 0 7.8' },
  { href: '/traces', label: 'Traces', icon: 'M3 12h4l3-8 4 16 3-8h4' },
  { href: '/logs', label: 'Logs', icon: 'M4 6h16M4 10h10M4 14h16M4 18h7' },
  { href: '/issues', label: 'Issues', icon: 'M12 9v4m0 4h.01M10.3 3.9 1.8 18a2 2 0 0 0 1.7 3h17a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0Z' },
  { href: '/metrics', label: 'Metrics', icon: 'M3 3v18h18M7 15l4-4 3 3 5-6' },
  { href: '/replays', label: 'Replays', icon: 'M4 5h16v14H4zM10 9l5 3-5 3z' },
  { href: '/releases', label: 'Releases', icon: 'M20.6 13.4 13.4 20.6a2 2 0 0 1-2.8 0L3 13V3h10l7.6 7.6a2 2 0 0 1 0 2.8ZM7.5 7.5h.01' },
  { href: '/alerts', label: 'Alerts', icon: 'M6 8a6 6 0 0 1 12 0c0 7 3 9 3 9H3s3-2 3-9M10.3 21a1.9 1.9 0 0 0 3.4 0' },
  { href: '/events', label: 'Events', icon: 'M4 6h16M4 12h16M4 18h10' },
  { href: '/funnels', label: 'Funnels', icon: 'M3 4h18l-7 9v6l-4 2v-8L3 4Z' },
  { href: '/settings', label: 'Settings', icon: 'M12 15a3 3 0 1 0 0-6 3 3 0 0 0 0 6Zm7.4-3a7.4 7.4 0 0 0-.1-1.2l2-1.6-2-3.4-2.4 1a7 7 0 0 0-2-1.2L14.5 3h-5l-.4 2.6a7 7 0 0 0-2 1.2l-2.4-1-2 3.4 2 1.6a7.4 7.4 0 0 0 0 2.4l-2 1.6 2 3.4 2.4-1a7 7 0 0 0 2 1.2l.4 2.6h5l.4-2.6a7 7 0 0 0 2-1.2l2.4 1 2-3.4-2-1.6c.1-.4.1-.8.1-1.2Z' },
];

export function Shell({ children }: { children: React.ReactNode }) {
  const path = usePathname();
  const router = useRouter();
  const [me, setMe] = useState<Me>();
  const [sent, setSent] = useState(false);
  useEffect(() => {
    api.me().then((m) => {
      const ctx = currentContext(m);
      if (ctx.org?.id !== storedOrg() || ctx.project?.id !== storedProject()) {
        // Remember a consistent pair; reload if pages may have queried another project.
        const reload = storedProject() !== undefined && ctx.project?.id !== storedProject();
        selectOrg(ctx.org?.id);
        selectProject(ctx.project?.id);
        if (reload) {
          window.location.reload();
          return;
        }
      }
      setMe(m);
      const here = window.location.pathname;
      if (!ctx.project && ctx.org?.role === 'owner' && !here.startsWith('/projects/new') && !here.startsWith('/settings')) router.replace('/projects/new');
    }, handleAuthError);
  }, [router]);
  const ctx = me ? currentContext(me) : undefined;
  const current = ctx?.project;
  const switchTo = (v: string) => {
    if (v === 'new') {
      router.push('/projects/new');
      return;
    }
    selectProject(Number(v));
    window.location.reload();
  };
  const switchOrg = (v: string) => {
    const id = Number(v);
    selectOrg(id);
    selectProject(me?.projects.find((p) => p.org_id === id)?.id);
    window.location.href = '/traces';
  };
  const resend = async () => {
    await api.resendVerification().catch(handleAuthError);
    setSent(true);
  };
  const logout = async () => {
    await api.logout().catch(() => undefined);
    window.location.href = '/login';
  };
  return (
    <div className="flex min-h-screen">
      <aside className="sticky top-0 flex h-screen w-56 shrink-0 flex-col border-r border-border bg-card/60 px-3 py-4">
        <Link href="/traces" className="mb-6 flex items-center gap-2 px-2">
          <span className="grid h-7 w-7 place-items-center rounded-md bg-primary text-sm font-bold">P</span>
          <span className="font-semibold tracking-tight">Pulse</span>
        </Link>
        {me && ctx && me.orgs.length > 0 && (
          <select
            aria-label="Organization"
            data-testid="org-switcher"
            className="mb-2 h-8 rounded-md border border-border bg-background px-2 text-xs font-medium text-zinc-100 outline-none focus:border-primary"
            value={ctx.org?.id ?? ''}
            onChange={(e) => switchOrg(e.target.value)}
          >
            {me.orgs.map((o) => (
              <option key={o.id} value={o.id}>
                {o.name}
              </option>
            ))}
          </select>
        )}
        {me && (
          <select
            aria-label="Project"
            data-testid="project-switcher"
            className="mb-4 h-8 rounded-md border border-border bg-background px-2 text-xs text-zinc-200 outline-none focus:border-primary"
            value={current?.id ?? ''}
            onChange={(e) => switchTo(e.target.value)}
          >
            {!current && <option value="">No project</option>}
            {(ctx?.projects ?? []).map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
              </option>
            ))}
            {ctx?.org?.role === 'owner' && <option value="new">+ New project</option>}
          </select>
        )}
        <nav className="flex flex-col gap-0.5">
          {links.map((l) => (
            <Link
              key={l.href}
              href={l.href}
              className={cn(
                'flex items-center gap-2.5 rounded-md px-2 py-1.5 text-sm text-zinc-400 hover:bg-accent hover:text-zinc-100',
                path.startsWith(l.href) && 'bg-accent text-zinc-100',
              )}
            >
              <svg viewBox="0 0 24 24" className="h-4 w-4" fill="none" stroke="currentColor" strokeWidth={1.8} strokeLinecap="round" strokeLinejoin="round">
                <path d={l.icon} />
              </svg>
              {l.label}
            </Link>
          ))}
        </nav>
        <div className="mt-auto border-t border-border px-2 pt-3 text-xs">
          <div className="truncate text-zinc-300" data-testid="user-email">{me?.user.email}</div>
          <button onClick={logout} className="mt-1 text-muted hover:text-zinc-100">
            Log out
          </button>
        </div>
      </aside>
      <main className="min-w-0 flex-1 px-8 py-6">
        {me && !me.user.email_verified && (
          <div className="mb-4 flex items-center justify-between gap-3 rounded-md border border-amber-500/30 bg-amber-500/10 px-4 py-2 text-sm text-amber-200" data-testid="verify-banner">
            <span>Confirm your email address: we sent a link to {me.user.email}.</span>
            <button onClick={resend} disabled={sent} className="shrink-0 text-xs font-medium underline-offset-2 hover:underline disabled:opacity-60">
              {sent ? 'Sent' : 'Resend email'}
            </button>
          </div>
        )}
        {children}
      </main>
    </div>
  );
}
