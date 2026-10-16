'use client';
import Link from 'next/link';
import { usePathname, useRouter } from 'next/navigation';
import { useEffect, useState } from 'react';
import type { Me } from '@pulse/api-client';
import { api, currentContext, handleAuthError, selectOrg, selectProject, storedOrg, storedProject } from '@/lib/api';
import { cn } from '@/lib/cn';
import { ThemeToggle } from '@/components/theme-toggle';

type NavLink = { href: string; label: string; icon: string };

const groups: { label: string; links: NavLink[] }[] = [
  {
    label: 'Product analytics',
    links: [
      { href: '/dashboards', label: 'Dashboards', icon: 'M3 3h8v8H3zM13 3h8v5h-8zM13 10h8v11h-8zM3 13h8v8H3z' },
      { href: '/insights', label: 'Insights', icon: 'M4 19V9m6 10V5m6 14v-7m4 7H2' },
      { href: '/funnels', label: 'Funnels', icon: 'M3 4h18l-7 9v6l-4 2v-8L3 4Z' },
      { href: '/retention', label: 'Retention', icon: 'M3 4h18v16H3zM3 10h18M9 4v16' },
      { href: '/paths', label: 'Paths', icon: 'M5 6h4l6 12h4M5 18h4l2-4M15 6h4' },
      { href: '/persons', label: 'Persons', icon: 'M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2M9 11a4 4 0 1 0 0-8 4 4 0 0 0 0 8ZM22 21v-2a4 4 0 0 0-3-3.9M16 3.1a4 4 0 0 1 0 7.8' },
      { href: '/events', label: 'Events', icon: 'M4 6h16M4 12h16M4 18h10' },
    ],
  },
  {
    label: 'Observability',
    links: [
      { href: '/traces', label: 'Traces', icon: 'M3 12h4l3-8 4 16 3-8h4' },
      { href: '/logs', label: 'Logs', icon: 'M4 6h16M4 10h10M4 14h16M4 18h7' },
      { href: '/metrics', label: 'Metrics', icon: 'M3 3v18h18M7 15l4-4 3 3 5-6' },
    ],
  },
  {
    label: 'Errors',
    links: [
      { href: '/issues', label: 'Issues', icon: 'M12 9v4m0 4h.01M10.3 3.9 1.8 18a2 2 0 0 0 1.7 3h17a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0Z' },
      { href: '/replays', label: 'Replays', icon: 'M4 5h16v14H4zM10 9l5 3-5 3z' },
      { href: '/releases', label: 'Releases', icon: 'M20.6 13.4 13.4 20.6a2 2 0 0 1-2.8 0L3 13V3h10l7.6 7.6a2 2 0 0 1 0 2.8ZM7.5 7.5h.01' },
    ],
  },
  {
    label: 'Operate',
    links: [
      { href: '/alerts', label: 'Alerts', icon: 'M6 8a6 6 0 0 1 12 0c0 7 3 9 3 9H3s3-2 3-9M10.3 21a1.9 1.9 0 0 0 3.4 0' },
      { href: '/settings', label: 'Settings', icon: 'M12 15a3 3 0 1 0 0-6 3 3 0 0 0 0 6Zm7.4-3a7.4 7.4 0 0 0-.1-1.2l2-1.6-2-3.4-2.4 1a7 7 0 0 0-2-1.2L14.5 3h-5l-.4 2.6a7 7 0 0 0-2 1.2l-2.4-1-2 3.4 2 1.6a7.4 7.4 0 0 0 0 2.4l-2 1.6 2 3.4 2.4-1a7 7 0 0 0 2 1.2l.4 2.6h5l.4-2.6a7 7 0 0 0 2-1.2l2.4 1 2-3.4-2-1.6c.1-.4.1-.8.1-1.2Z' },
    ],
  },
];

export function Shell({ children }: { children: React.ReactNode }) {
  const path = usePathname();
  const router = useRouter();
  const [me, setMe] = useState<Me>();
  const [sent, setSent] = useState(false);
  const [open, setOpen] = useState(false);
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
  const active = groups.flatMap((g) => g.links).find((l) => path.startsWith(l.href));
  const selectCls = 'h-8 w-full rounded-md border border-border bg-surface px-2 text-xs font-medium text-fg outline-none transition-colors hover:border-strong focus:border-primary/70';
  return (
    <div className="flex min-h-screen">
      <a href="#main" className="sr-only focus:not-sr-only focus:fixed focus:left-2 focus:top-2 focus:z-50 focus:rounded-md focus:bg-surface focus:px-3 focus:py-1.5 focus:shadow-pop">
        Skip to content
      </a>
      {open && <div className="fixed inset-0 z-30 bg-black/40 lg:hidden" onClick={() => setOpen(false)} aria-hidden />}
      <aside
        id="sidebar"
        className={cn(
          'fixed inset-y-0 left-0 z-40 w-60 shrink-0 flex-col border-r border-border bg-surface lg:sticky lg:top-0 lg:z-auto lg:flex lg:h-screen lg:w-56 lg:bg-background',
          open ? 'flex shadow-pop' : 'hidden',
        )}
      >
        <div className="flex h-12 items-center justify-between gap-2 border-b border-border px-3">
          <Link href="/traces" className="flex items-center gap-2 rounded-md px-1 py-1">
            <Logo />
            <span className="text-[13px] font-semibold tracking-tight">Pulse</span>
          </Link>
          <button type="button" className="grid h-7 w-7 place-items-center rounded-md text-muted hover:bg-accent lg:hidden" onClick={() => setOpen(false)} aria-label="Close navigation">
            <Icon d="M6 6l12 12M18 6 6 18" />
          </button>
        </div>
        <div className="flex flex-col gap-1.5 border-b border-border p-3">
          {me && ctx && me.orgs.length > 0 && (
            <select aria-label="Organization" data-testid="org-switcher" className={selectCls} value={ctx.org?.id ?? ''} onChange={(e) => switchOrg(e.target.value)}>
              {me.orgs.map((o) => (
                <option key={o.id} value={o.id}>
                  {o.name}
                </option>
              ))}
            </select>
          )}
          {me ? (
            <select aria-label="Project" data-testid="project-switcher" className={cn(selectCls, 'font-normal text-fg-2')} value={current?.id ?? ''} onChange={(e) => switchTo(e.target.value)}>
              {!current && <option value="">No project</option>}
              {(ctx?.projects ?? []).map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name}
                </option>
              ))}
              {ctx?.org?.role === 'owner' && <option value="new">+ New project</option>}
            </select>
          ) : (
            <div className="h-8 animate-pulse rounded-md bg-accent" aria-hidden />
          )}
        </div>
        <nav aria-label="Main" className="flex-1 overflow-y-auto px-2 py-2">
          {groups.map((g) => (
            <div key={g.label} className="mb-3">
              <div className="px-2 pb-1 pt-1 text-[10.5px] font-medium uppercase tracking-wider text-subtle">{g.label}</div>
              <div className="flex flex-col gap-px">
                {g.links.map((l) => {
                  const on = path.startsWith(l.href);
                  return (
                    <Link
                      key={l.href}
                      href={l.href}
                      onClick={() => setOpen(false)}
                      aria-current={on ? 'page' : undefined}
                      className={cn(
                        'relative flex h-7 items-center gap-2.5 rounded-md px-2 text-[13px] text-muted transition-colors hover:bg-accent hover:text-fg',
                        on && 'bg-accent font-medium text-fg before:absolute before:-left-2 before:top-1.5 before:h-4 before:w-0.5 before:rounded-r before:bg-primary',
                      )}
                    >
                      <Icon d={l.icon} className={on ? 'text-primary' : undefined} />
                      {l.label}
                    </Link>
                  );
                })}
              </div>
            </div>
          ))}
        </nav>
        <div className="border-t border-border p-3 text-xs">
          <div className="flex items-center gap-2">
            <span className="grid h-6 w-6 shrink-0 place-items-center rounded-full bg-accent text-[11px] font-semibold uppercase text-fg-2" aria-hidden>
              {me?.user.email?.[0] ?? ''}
            </span>
            <div className="min-w-0 flex-1 truncate text-fg-2" data-testid="user-email">
              {me?.user.email}
            </div>
          </div>
          <div className="mt-2.5 flex items-center justify-between">
            <ThemeToggle />
            <button onClick={logout} className="rounded px-1.5 py-1 text-muted hover:bg-accent hover:text-fg">
              Log out
            </button>
          </div>
        </div>
      </aside>
      <div className="flex min-w-0 flex-1 flex-col">
        <header className="sticky top-0 z-20 flex h-12 items-center gap-2 border-b border-border bg-background/95 px-3 backdrop-blur lg:hidden">
          <button
            type="button"
            className="grid h-8 w-8 place-items-center rounded-md text-fg-2 hover:bg-accent"
            onClick={() => setOpen(true)}
            aria-label="Open navigation"
            aria-controls="sidebar"
            aria-expanded={open}
          >
            <Icon d="M4 6h16M4 12h16M4 18h16" />
          </button>
          <Logo />
          <span className="text-[13px] font-semibold">Pulse</span>
          {active && <span className="truncate text-[13px] text-muted">/ {active.label}</span>}
        </header>
        <main id="main" className="mx-auto w-full min-w-0 max-w-[1600px] flex-1 px-4 py-5 sm:px-6 lg:px-8 lg:py-6">
          {me && !me.user.email_verified && (
            <div className="mb-4 flex flex-wrap items-center justify-between gap-3 rounded-md border border-warn/30 bg-warn/10 px-3 py-2 text-[13px] text-warn-fg" data-testid="verify-banner">
              <span>Confirm your email address: we sent a link to {me.user.email}.</span>
              <button onClick={resend} disabled={sent} className="shrink-0 text-xs font-medium underline-offset-2 hover:underline disabled:opacity-60">
                {sent ? 'Sent' : 'Resend email'}
              </button>
            </div>
          )}
          {children}
        </main>
      </div>
    </div>
  );
}

function Icon({ d, className }: { d: string; className?: string }) {
  return (
    <svg viewBox="0 0 24 24" className={cn('h-4 w-4 shrink-0', className)} fill="none" stroke="currentColor" strokeWidth={1.7} strokeLinecap="round" strokeLinejoin="round" aria-hidden>
      <path d={d} />
    </svg>
  );
}

/** The Pulse mark: a heartbeat line in a rounded square. */
export function Logo({ size = 24 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" aria-hidden className="shrink-0">
      <rect width="24" height="24" rx="6" className="fill-fg" />
      <path d="M4.5 12.5h3.2l2-4.5 3.6 9 2-4.5h4.2" fill="none" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round" className="stroke-background" />
    </svg>
  );
}
