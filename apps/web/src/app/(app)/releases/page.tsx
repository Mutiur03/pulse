'use client';
import Link from 'next/link';
import { api, currentProjectId } from '@/lib/api';
import { ago, time } from '@/lib/format';
import { useAsync, useQueryState } from '@/lib/hooks';
import { Badge, Card, CardHeader, Empty, ErrorNote, PageHeader, Spinner, Td, Th } from '@/components/ui';

function size(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1 << 20) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1 << 20)).toFixed(1)} MB`;
}

export default function ReleasesPage() {
  const [q, setQ] = useQueryState(['release']);
  const list = useAsync(async () => {
    const project = await currentProjectId();
    return { project, releases: await api.listReleases(project, 100) };
  }, []);
  const selected = q.release || list.data?.releases[0]?.version;
  const detail = useAsync(
    () => (list.data && selected ? api.getRelease(list.data.project, selected) : Promise.resolve(undefined)),
    [list.data?.project, selected],
  );

  return (
    <>
      <PageHeader title="Releases" description="Versions your apps report, with the source maps uploaded for each." />
      <ErrorNote error={list.error ?? detail.error} />
      {list.loading && !list.data && <Spinner />}
      {list.data && list.data.releases.length === 0 && (
        <Empty>
          No releases yet. Set <code className="font-mono">release</code> in the SDK and upload source maps with pulse-cli or
          @pulse/vite-plugin, using a token from{' '}
          <Link href="/settings" className="text-primary hover:underline">
            Settings
          </Link>
          .
        </Empty>
      )}
      {list.data && list.data.releases.length > 0 && (
        <div className="grid gap-4 xl:grid-cols-[360px_1fr]">
          <Card className="h-fit overflow-hidden">
            <table className="w-full text-sm">
              <thead className="border-b border-border">
                <tr>
                  <Th>Release</Th>
                  <Th>Maps</Th>
                  <Th>Created</Th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border">
                {list.data.releases.map((r) => (
                  <tr
                    key={r.id}
                    role="button"
                    tabIndex={0}
                    onClick={() => setQ({ release: r.version })}
                    onKeyDown={(e) => e.key === 'Enter' && setQ({ release: r.version })}
                    className={`hover:bg-accent/50 ${r.version === selected ? 'bg-accent' : ''}`}
                    data-testid="release-row"
                  >
                    <Td className="font-mono">{r.version}</Td>
                    <Td>{r.artifact_count}</Td>
                    <Td className="whitespace-nowrap text-muted">{ago(r.created_at)}</Td>
                  </tr>
                ))}
              </tbody>
            </table>
          </Card>
          <Card className="overflow-hidden">
            <CardHeader
              title={selected ?? ''}
              description={detail.data ? `Created ${time(detail.data.release.created_at)}` : undefined}
            />
            {detail.loading && !detail.data && (
              <div className="p-4">
                <Spinner />
              </div>
            )}
            {detail.data && detail.data.artifacts.length === 0 && <div className="p-4 text-sm text-muted">No source maps uploaded.</div>}
            {detail.data && detail.data.artifacts.length > 0 && (
              <table className="w-full text-xs" data-testid="artifacts">
                <thead className="border-b border-border">
                  <tr>
                    <Th>Artifact</Th>
                    <Th>Debug id</Th>
                    <Th>Size</Th>
                    <Th>Uploaded</Th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-border">
                  {detail.data.artifacts.map((a) => (
                    <tr key={a.id} data-testid="artifact-row">
                      <Td className="font-mono">{a.name}</Td>
                      <Td className="font-mono">{a.debug_id ? <Badge tone="ok">{a.debug_id.slice(0, 8)}</Badge> : <span className="text-muted">URL match</span>}</Td>
                      <Td className="whitespace-nowrap">{size(a.size)}</Td>
                      <Td className="whitespace-nowrap text-muted">{ago(a.created_at)}</Td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </Card>
        </div>
      )}
    </>
  );
}
