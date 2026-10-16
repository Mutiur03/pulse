'use client';
import Link from 'next/link';
import { Suspense, useEffect, useState } from 'react';
import { useSearchParams } from 'next/navigation';
import { APIError, type InviteInfo, type Me } from '@pulse/api-client';
import { api, selectOrg, selectProject } from '@/lib/api';
import { AuthCard } from '@/components/auth-card';
import { Button, ErrorNote, Input, Spinner } from '@/components/ui';

function message(err: unknown, fallback: string) {
  return err instanceof APIError && err.status < 500 ? err.message.charAt(0).toUpperCase() + err.message.slice(1) + '.' : fallback;
}

function Invite() {
  const token = useSearchParams().get('token') ?? '';
  const [info, setInfo] = useState<InviteInfo>();
  const [me, setMe] = useState<Me | null>();
  const [error, setError] = useState<string>();
  const [busy, setBusy] = useState(false);
  const [name, setName] = useState('');
  const [password, setPassword] = useState('');

  useEffect(() => {
    api.lookupInvite(token).then(setInfo, (e) => setError(message(e, 'Could not load the invite.')));
    api.me().then(setMe, () => setMe(null));
  }, [token]);

  const enter = (orgId: number) => {
    selectOrg(orgId);
    selectProject(undefined);
    window.location.href = '/traces';
  };
  const accept = async () => {
    if (!info) return;
    setBusy(true);
    setError(undefined);
    try {
      await api.acceptInvite(token);
      enter(info.org_id);
    } catch (e) {
      setError(message(e, 'Could not accept the invite.'));
      setBusy(false);
    }
  };
  const signup = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!info) return;
    if (password.length < 10) {
      setError('Password must be at least 10 characters.');
      return;
    }
    setBusy(true);
    setError(undefined);
    try {
      await api.signup(info.email, password, name, token);
      enter(info.org_id);
    } catch (e) {
      setError(
        e instanceof APIError && e.status === 409 ? 'You already have an account. Sign in to accept the invite.' : message(e, 'Could not create the account.'),
      );
      setBusy(false);
    }
  };
  const next = `/invite?token=${encodeURIComponent(token)}`;

  if (!info || me === undefined) {
    return (
      <AuthCard title="Join an organization">
        {error ? <ErrorNote error={error} /> : <Spinner />}
        {error && (
          <p className="mt-4 text-xs">
            <Link href="/login" className="text-fg underline-offset-2 hover:underline">
              Go to sign in
            </Link>
          </p>
        )}
      </AuthCard>
    );
  }
  return (
    <AuthCard title={`Join ${info.org_name}`} subtitle={`${info.inviter || 'An owner'} invited ${info.email} as ${info.role}.`}>
      <div data-testid="invite-card">
        {me ? (
          <div className="flex flex-col gap-3 text-sm text-fg-2">
            <p>
              Signed in as <span className="font-medium">{me.user.email}</span>.
            </p>
            {me.user.email.toLowerCase() !== info.email.toLowerCase() && (
              <p className="text-xs text-warn-fg">This invite is for {info.email}. Sign in with that account to accept it.</p>
            )}
            <ErrorNote error={error} />
            <Button onClick={accept} disabled={busy}>
              {busy ? 'Joining…' : `Accept and join ${info.org_name}`}
            </Button>
            <button
              className="text-xs text-muted hover:text-fg"
              onClick={async () => {
                await api.logout().catch(() => undefined);
                window.location.href = `/login?next=${encodeURIComponent(next)}`;
              }}
            >
              Use another account
            </button>
          </div>
        ) : (
          <>
            <form onSubmit={signup} className="flex flex-col gap-3">
              <label className="flex flex-col gap-1 text-xs text-muted">
                Email
                <Input type="email" value={info.email} readOnly disabled />
              </label>
              <label className="flex flex-col gap-1 text-xs text-muted">
                Name
                <Input name="name" autoComplete="name" required maxLength={100} value={name} onChange={(e) => setName(e.target.value)} />
              </label>
              <label className="flex flex-col gap-1 text-xs text-muted">
                Password
                <Input type="password" name="password" autoComplete="new-password" required value={password} onChange={(e) => setPassword(e.target.value)} />
                <span className="text-[11px]">At least 10 characters.</span>
              </label>
              <ErrorNote error={error} />
              <Button type="submit" disabled={busy} className="mt-2">
                {busy ? 'Creating account…' : 'Create account and join'}
              </Button>
            </form>
            <p className="mt-4 text-xs text-muted">
              Already have an account?{' '}
              <Link href={`/login?next=${encodeURIComponent(next)}`} className="text-fg underline-offset-2 hover:underline">
                Sign in to accept
              </Link>
            </p>
          </>
        )}
      </div>
    </AuthCard>
  );
}

export default function InvitePage() {
  return (
    <Suspense>
      <Invite />
    </Suspense>
  );
}
