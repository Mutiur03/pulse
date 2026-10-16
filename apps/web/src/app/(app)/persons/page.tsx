'use client';
import Link from 'next/link';
import { useState } from 'react';
import { api } from '@/lib/api';
import { ago } from '@/lib/format';
import { displayName } from '@/lib/person';
import { useAsync, useQueryState } from '@/lib/hooks';
import { Badge, Card, Empty, ErrorNote, Input, PageHeader, Spinner, Td, Th } from '@/components/ui';

export default function PersonsPage() {
  const [q, setQ] = useQueryState(['search']);
  const [search, setSearch] = useState<string>();
  const list = useAsync(() => api.listPersons({ search: q.search || undefined, limit: 100 }), [q.search]);
  return (
    <>
      <PageHeader
        title="Persons"
        description="Everyone who used your app in the last 30 days. Anonymous visits merge into the identified user after identify() or alias()."
      />
      <form
        className="mb-4"
        onSubmit={(e) => {
          e.preventDefault();
          setQ({ search: (search ?? q.search ?? '').trim() });
        }}
      >
        <Input aria-label="Search persons" placeholder="Search by id, email or any trait" value={search ?? q.search ?? ''} onChange={(e) => setSearch(e.target.value)} className="w-96" />
      </form>
      <ErrorNote error={list.error} />
      {list.loading && !list.data && <Spinner />}
      {list.data && (
        <Card>
          {list.data.persons.length === 0 ? (
            <Empty>{q.search ? 'No one matches this search.' : 'No persons yet. Send events with the browser SDK to see people here.'}</Empty>
          ) : (
            <table className="w-full text-sm" data-testid="persons-table">
              <thead className="border-b border-border">
                <tr>
                  <Th>Person</Th>
                  <Th>Ids</Th>
                  <Th>Location and device</Th>
                  <Th className="text-right">Sessions</Th>
                  <Th className="text-right">Events</Th>
                  <Th>Last seen</Th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border">
                {list.data.persons.map((p) => (
                  <tr key={p.id} className="hover:bg-accent/40" data-testid="person-row">
                    <Td>
                      <Link href={`/persons/profile?id=${encodeURIComponent(p.id)}`} className="font-medium hover:text-primary">
                        {displayName(p)}
                      </Link>
                      {displayName(p) !== p.id && <div className="font-mono text-[11px] text-muted">{p.id}</div>}
                    </Td>
                    <Td>
                      <Badge>{p.distinct_ids.length}</Badge>
                      {p.distinct_ids.length > 1 && <span className="ml-1.5 text-xs text-muted">merged</span>}
                    </Td>
                    <Td className="text-xs text-zinc-300">{[p.country, p.browser, p.os].filter(Boolean).join(' · ') || '—'}</Td>
                    <Td className="text-right tabular-nums">{p.sessions}</Td>
                    <Td className="text-right tabular-nums">{p.events}</Td>
                    <Td className="text-xs text-muted">{ago(p.last_seen)}</Td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </Card>
      )}
    </>
  );
}
