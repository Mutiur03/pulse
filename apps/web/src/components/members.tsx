'use client';
import { useState } from 'react';
import type { Org, Role } from '@pulse/api-client';
import { api, handleAuthError } from '@/lib/api';
import { time } from '@/lib/format';
import { useAsync } from '@/lib/hooks';
import { Badge, Button, Card, CardHeader, ErrorNote, Input, Select } from '@/components/ui';

/** Organization members and pending invites; owners can invite and remove. */
export function Members({ org, me }: { org: Org; me: number }) {
  const owner = org.role === 'owner';
  const members = useAsync(() => api.listMembers(org.id), [org.id]);
  const invites = useAsync(() => (owner ? api.listInvites(org.id) : Promise.resolve([])), [org.id, owner]);
  const [email, setEmail] = useState('');
  const [role, setRole] = useState<Role>('member');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string>();
  const [note, setNote] = useState<string>();

  const act = async (fn: () => Promise<unknown>, done?: string) => {
    setBusy(true);
    setErr(undefined);
    setNote(undefined);
    try {
      await fn();
      members.reload();
      invites.reload();
      if (done) setNote(done);
    } catch (e) {
      handleAuthError(e);
      setErr(e instanceof Error ? e.message : String(e));
    }
    setBusy(false);
  };
  const invite = (e: React.FormEvent) => {
    e.preventDefault();
    const to = email.trim();
    void act(async () => {
      await api.createInvite(org.id, to, role);
      setEmail('');
    }, `Invite sent to ${to}.`);
  };

  return (
    <Card className="mt-4">
      <CardHeader title={`Members of ${org.name}`} description="Everyone in the organization can see all of its projects. Owners manage members, projects and keys." />
      <ErrorNote error={members.error ?? invites.error ?? err} />
      <div className="divide-y divide-border" data-testid="members">
        {members.data?.map((m) => (
          <div key={m.user_id} className="flex items-center justify-between gap-3 px-4 py-2.5 text-sm" data-testid="member-row">
            <div className="min-w-0">
              <div className="truncate text-zinc-200">
                {m.name} <span className="text-muted">· {m.email}</span>
              </div>
              <div className="text-xs text-muted">Joined {time(m.joined_at)}</div>
            </div>
            <div className="flex shrink-0 items-center gap-2">
              <Badge tone={m.role === 'owner' ? 'backend' : 'neutral'}>{m.role}</Badge>
              {owner && m.user_id !== me && (
                <Button
                  size="sm"
                  variant="ghost"
                  disabled={busy}
                  onClick={() => confirm(`Remove ${m.email} from ${org.name}?`) && void act(() => api.removeMember(org.id, m.user_id))}
                >
                  Remove
                </Button>
              )}
            </div>
          </div>
        ))}
      </div>
      {owner && (
        <>
          <div className="border-t border-border px-4 py-3 text-xs font-medium text-zinc-300">Pending invites</div>
          <div className="divide-y divide-border">
            {invites.data?.length === 0 && <div className="px-4 pb-3 text-xs text-muted">None.</div>}
            {invites.data?.map((i) => (
              <div key={i.id} className="flex items-center justify-between gap-3 px-4 py-2.5 text-sm" data-testid="invite-row">
                <div className="min-w-0">
                  <div className="truncate text-zinc-200">{i.email}</div>
                  <div className="text-xs text-muted">
                    {i.role} · expires {time(i.expires_at)}
                  </div>
                </div>
                <Button size="sm" variant="ghost" disabled={busy} onClick={() => void act(() => api.revokeInvite(org.id, i.id))}>
                  Revoke
                </Button>
              </div>
            ))}
          </div>
          <form onSubmit={invite} className="flex flex-wrap items-center gap-2 border-t border-border p-4">
            <Input
              type="email"
              aria-label="Invite email"
              placeholder="teammate@example.com"
              required
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              className="max-w-xs"
            />
            <Select aria-label="Invite role" value={role} onChange={(e) => setRole(e.target.value as Role)} className="w-28">
              <option value="member">member</option>
              <option value="owner">owner</option>
            </Select>
            <Button type="submit" size="sm" disabled={busy || !email.trim()}>
              Send invite
            </Button>
            {note && <span className="text-xs text-emerald-400">{note}</span>}
          </form>
        </>
      )}
    </Card>
  );
}
