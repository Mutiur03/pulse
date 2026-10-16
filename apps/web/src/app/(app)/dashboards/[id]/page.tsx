'use client';
import { useParams, useRouter } from 'next/navigation';
import { useEffect, useState } from 'react';
import type { Dashboard, DashboardWidget } from '@pulse/api-client';
import { api, currentProjectId, handleAuthError } from '@/lib/api';
import { useAsync } from '@/lib/hooks';
import { Badge, Button, Card, Empty, ErrorNote, Input, PageHeader, Select, Spinner, Toolbar } from '@/components/ui';
import { WidgetCard, WidgetEditor, dashboardRanges, defaultQuery } from '@/components/widgets';

export default function DashboardPage() {
  const { id } = useParams<{ id: string }>();
  const router = useRouter();
  const [board, setBoard] = useState<Dashboard>();
  const [error, setError] = useState<string>();
  const [range, setRange] = useState('7d');
  const [editing, setEditing] = useState(false);
  const [editor, setEditor] = useState<DashboardWidget>();
  const [name, setName] = useState('');
  const since = new Date(Date.now() - 30 * 86400e3).toISOString();
  const events = useAsync(() => api.listEventNames({ from: since }), []);
  const metrics = useAsync(() => api.listMetricNames({ from: since }), []);
  const services = useAsync(() => api.listServices({ from: since }), []);
  const eventNames = events.data?.map((e) => e.name) ?? [];

  useEffect(() => {
    (async () => {
      try {
        const d = await api.getDashboard(await currentProjectId(), Number(id));
        setBoard(d);
        setName(d.name);
        if (new URLSearchParams(window.location.search).get('edit') && d.can_edit) setEditing(true);
      } catch (e) {
        handleAuthError(e);
        setError(e instanceof Error ? e.message : String(e));
      }
    })();
  }, [id]);

  const save = async (patch: Partial<Dashboard>) => {
    if (!board) return;
    const next = { ...board, ...patch };
    setError(undefined);
    try {
      const d = await api.updateDashboard(board.project_id, board.id, { name: next.name, description: next.description, shared: next.shared, widgets: next.widgets });
      setBoard(d);
      return true;
    } catch (e) {
      handleAuthError(e);
      setError(e instanceof Error ? e.message : String(e));
      return false;
    }
  };

  const remove = async () => {
    if (!board || !window.confirm(`Delete ${board.name}?`)) return;
    await api.deleteDashboard(board.project_id, board.id).catch(handleAuthError);
    router.push('/dashboards');
  };

  if (!board) return error ? <ErrorNote error={error} /> : <Spinner />;
  const saveWidget = async (w: DashboardWidget) => {
    const exists = board.widgets.some((x) => x.id === w.id);
    const widgets = exists ? board.widgets.map((x) => (x.id === w.id ? w : x)) : [...board.widgets, w];
    if (await save({ widgets })) setEditor(undefined);
  };

  return (
    <>
      <PageHeader
        title={board.name}
        description={`${board.shared ? 'Shared with the organization' : 'Private'} · by ${board.creator_name || 'unknown'}`}
        actions={
          <div className="flex flex-wrap items-center gap-2">
            <Select aria-label="Dashboard range" value={range} onChange={(e) => setRange(e.target.value)} data-testid="dashboard-range">
              {Object.keys(dashboardRanges).map((r) => (
                <option key={r} value={r}>
                  Last {r}
                </option>
              ))}
            </Select>
            {board.can_edit && (
              <Button variant={editing ? 'default' : 'outline'} onClick={() => setEditing(!editing)} data-testid="toggle-edit">
                {editing ? 'Done' : 'Edit'}
              </Button>
            )}
            {!board.can_edit && <Badge>View only</Badge>}
          </div>
        }
      />
      <ErrorNote error={error} />
      {editing && (
        <Toolbar>
          <Input aria-label="Rename dashboard" value={name} onChange={(e) => setName(e.target.value)} className="w-64" />
          <Button size="sm" variant="outline" onClick={() => save({ name })} data-testid="rename-dashboard">
            Rename
          </Button>
          <label className="ml-2 flex items-center gap-1.5 text-sm text-fg-2">
            <input type="checkbox" checked={board.shared} onChange={(e) => save({ shared: e.target.checked })} data-testid="share-dashboard" />
            Share with organization
          </label>
          <div className="ml-auto flex gap-2">
            <Button
              size="sm"
              onClick={() => setEditor({ id: `w${Date.now().toString(36)}`, kind: 'trend', title: '', width: 1, query: defaultQuery('trend', eventNames) })}
              data-testid="add-widget"
            >
              + Add widget
            </Button>
            <Button size="sm" variant="danger" onClick={remove} data-testid="delete-dashboard">
              Delete
            </Button>
          </div>
        </Toolbar>
      )}
      {editor && (
        <WidgetEditor
          key={editor.id}
          initial={editor}
          events={eventNames}
          metrics={metrics.data?.map((m) => m.name) ?? []}
          services={services.data ?? []}
          onSave={saveWidget}
          onCancel={() => setEditor(undefined)}
        />
      )}
      {board.widgets.length === 0 ? (
        <Card>
          <Empty>{board.can_edit ? 'No widgets yet. Click Edit, then Add widget.' : 'This dashboard has no widgets yet.'}</Empty>
        </Card>
      ) : (
        <div className="grid gap-4 lg:grid-cols-2" data-testid="widget-grid">
          {board.widgets.map((w) => (
            <WidgetCard
              key={w.id}
              widget={w}
              range={range}
              editing={editing}
              onEdit={() => setEditor(w)}
              onRemove={() => save({ widgets: board.widgets.filter((x) => x.id !== w.id) })}
            />
          ))}
        </div>
      )}
    </>
  );
}
