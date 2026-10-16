'use client';
import Link from 'next/link';
import { useRouter } from 'next/navigation';
import { useState } from 'react';
import { api, currentProjectId, handleAuthError } from '@/lib/api';
import { ago } from '@/lib/format';
import { useAsync } from '@/lib/hooks';
import { Badge, Button, Card, CardHeader, Empty, ErrorNote, Input, Loading, PageHeader } from '@/components/ui';

export default function DashboardsPage() {
  const router = useRouter();
  const list = useAsync(async () => api.listDashboards(await currentProjectId()), []);
  const [name, setName] = useState('');
  const [shared, setShared] = useState(true);
  const [error, setError] = useState<string>();

  const create = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(undefined);
    try {
      const d = await api.createDashboard(await currentProjectId(), { name, shared, widgets: [] });
      router.push(`/dashboards/${d.id}?edit=1`);
    } catch (err) {
      handleAuthError(err);
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  return (
    <>
      <PageHeader title="Dashboards" description="Saved trends, funnels, metrics and trace stats on one page. Share a dashboard to show it to everyone in the organization." />
      <Card className="mb-4 p-4">
        <form onSubmit={create} className="flex flex-wrap items-center gap-2">
          <Input aria-label="Dashboard name" value={name} onChange={(e) => setName(e.target.value)} placeholder="Dashboard name" className="w-72" required />
          <label className="flex items-center gap-1.5 text-sm text-zinc-300">
            <input type="checkbox" checked={shared} onChange={(e) => setShared(e.target.checked)} aria-label="Share with organization" />
            Share with organization
          </label>
          <Button type="submit" data-testid="create-dashboard">
            Create dashboard
          </Button>
        </form>
        <ErrorNote error={error} />
      </Card>
      <ErrorNote error={list.error} />
      {list.loading && !list.data && (
        <Card>
          <Loading rows={3} />
        </Card>
      )}
      {list.data && (
        <Card>
          <CardHeader title="Your dashboards" description="Shared dashboards and your private ones" />
          {list.data.length === 0 ? (
            <Empty title="No dashboards yet" action={{ label: 'Open Insights', href: '/insights' }}>
              Create one above, or save a chart from Insights.
            </Empty>
          ) : (
            <ul className="divide-y divide-border" data-testid="dashboard-list">
              {list.data.map((d) => (
                <li key={d.id}>
                  <Link href={`/dashboards/${d.id}`} className="flex items-center justify-between gap-3 px-4 py-3 hover:bg-accent/40" data-testid="dashboard-row">
                    <div className="min-w-0">
                      <div className="truncate text-sm font-medium">{d.name}</div>
                      <div className="text-xs text-muted">
                        {d.widgets.length} widget{d.widgets.length === 1 ? '' : 's'} · by {d.creator_name || 'unknown'} · updated {ago(d.updated_at)}
                      </div>
                    </div>
                    <Badge tone={d.shared ? 'ok' : 'neutral'}>{d.shared ? 'Shared' : 'Private'}</Badge>
                  </Link>
                </li>
              ))}
            </ul>
          )}
        </Card>
      )}
    </>
  );
}
