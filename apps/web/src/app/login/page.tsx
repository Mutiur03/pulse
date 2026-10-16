'use client';
import Link from 'next/link';
import { Suspense, useState } from 'react';
import { useSearchParams } from 'next/navigation';
import { APIError } from '@pulse/api-client';
import { api } from '@/lib/api';
import { Button, Card, ErrorNote, Input } from '@/components/ui';
import { AuthFrame, AuthHeading } from '@/components/auth-card';

function LoginForm() {
  const params = useSearchParams();
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState<string>();
  const [busy, setBusy] = useState(false);
  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError(undefined);
    try {
      await api.login(email, password);
      const next = params.get('next');
      // Only follow same-site relative paths.
      window.location.href = next && next.startsWith('/') && !next.startsWith('//') ? next : '/traces';
    } catch (err) {
      if (err instanceof APIError && err.status === 401) setError('Wrong email or password.');
      else if (err instanceof APIError && err.status === 429) setError('Too many failed attempts. Try again in a few minutes.');
      else setError('Could not log in. Is the query API running?');
      setBusy(false);
    }
  };
  return (
    <Card className="w-full max-w-sm p-6 shadow-sm">
      <AuthHeading title="Sign in to Pulse" subtitle="Traces, errors and funnels in one place" />
      <form onSubmit={submit} className="flex flex-col gap-3">
        <label className="flex flex-col gap-1 text-xs text-muted">
          Email
          <Input type="email" name="email" autoComplete="username" required value={email} onChange={(e) => setEmail(e.target.value)} />
        </label>
        <label className="flex flex-col gap-1 text-xs text-muted">
          Password
          <Input type="password" name="password" autoComplete="current-password" required value={password} onChange={(e) => setPassword(e.target.value)} />
        </label>
        <Link href="/reset-password" className="-mt-1 self-end text-xs text-muted underline-offset-2 hover:text-fg hover:underline">
          Forgot password?
        </Link>
        <ErrorNote error={error} />
        <Button type="submit" disabled={busy} className="mt-2">
          {busy ? 'Signing in…' : 'Sign in'}
        </Button>
      </form>
      <p className="mt-4 text-xs text-muted">
        New to Pulse?{' '}
        <Link href="/signup" className="text-fg underline-offset-2 hover:underline">
          Create an account
        </Link>
      </p>
      <p className="mt-4 rounded-md border border-dashed border-border px-2.5 py-2 text-xs text-muted">
        Local stack: <span className="font-mono text-fg-2">demo@pulse.local</span> / <span className="font-mono text-fg-2">pulse-demo</span>
      </p>
    </Card>
  );
}

export default function LoginPage() {
  return (
    <AuthFrame>
      <Suspense>
        <LoginForm />
      </Suspense>
    </AuthFrame>
  );
}
