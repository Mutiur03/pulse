'use client';
import Link from 'next/link';
import { usePathname, useRouter } from 'next/navigation';
import { useEffect, useState } from 'react';
import type { Me } from '@pulse/api-client';
import { api, handleAuthError, selectProject, storedProject } from '@/lib/api';
import { cn } from '@/lib/cn';

const links = [
  { href: '/traces', label: 'Traces', icon: 'M3 12h4l3-8 4 16 3-8h4' },
  { href: '/issues', label: 'Issues', icon: 'M12 9v4m0 4h.01M10.3 3.9 1.8 18a2 2 0 0 0 1.7 3h17a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0Z' },
  { href: '/events', label: 'Events', icon: 'M4 6h16M4 12h16M4 18h10' },
  { href: '/funnels', label: 'Funnels', icon: 'M3 4h18l-7 9v6l-4 2v-8L3 4Z' },
  { href: '/settings', label: 'Settings', icon: 'M12 15a3 3 0 1 0 0-6 3 3 0 0 0 0 6Zm7.4-3a7.4 7.4 0 0 0-.1-1.2l2-1.6-2-3.4-2.4 1a7 7 0 0 0-2-1.2L14.5 3h-5l-.4 2.6a7 7 0 0 0-2 1.2l-2.4-1-2 3.4 2 1.6a7.4 7.4 0 0 0 0 2.4l-2 1.6 2 3.4 2.4-1a7 7 0 0 0 2 1.2l.4 2.6h5l.4-2.6a7 7 0 0 0 2-1.2l2.4 1 2-3.4-2-1.6c.1-.4.1-.8.1-1.2Z' },
];

export function Shell({ children }: { children: React.ReactNode }) {
  const path = usePathname();
  const router = useRouter();
  const [me, setMe] = useState<Me>();
  useEffect(() => {
    api.me().then((m) => {
      const stored = storedProject();
      if (stored !== undefined && !m.projects.some((p) => p.id === stored)) {
        // The remembered project is gone or belongs to another account.
        selectProject(undefined);
        window.location.reload();
        return;
      }
      setMe(m);
      if (m.projects.length === 0 && !window.location.pathname.startsWith('/projects/new')) router.replace('/projects/new');
    }, handleAuthError);
  }, [router]);
  const current = me?.projects.find((p) => p.id === storedProject()) ?? me?.projects[0];
  const switchTo = (v: string) => {
    if (v === 'new') {
      router.push('/projects/new');
      return;
    }
    selectProject(Number(v));
    window.location.reload();
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
        {me && (
          <select
            aria-label="Project"
            data-testid="project-switcher"
            className="mb-4 h-8 rounded-md border border-border bg-background px-2 text-xs text-zinc-200 outline-none focus:border-primary"
            value={current?.id ?? ''}
            onChange={(e) => switchTo(e.target.value)}
          >
            {!current && <option value="">No project</option>}
            {me.projects.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
              </option>
            ))}
            <option value="new">+ New project</option>
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
      <main className="min-w-0 flex-1 px-8 py-6">{children}</main>
    </div>
  );
}
