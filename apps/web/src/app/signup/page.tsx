'use client';
import Link from 'next/link';
import { useState } from 'react';
import { APIError } from '@pulse/api-client';
import { api, selectOrg, selectProject } from '@/lib/api';
import { Button, Card, ErrorNote, Input } from '@/components/ui';
import { AuthFrame, AuthHeading } from '@/components/auth-card';

export default function SignupPage() {
  const [name, setName] = useState('');
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState<string>();
  const [busy, setBusy] = useState(false);
  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (password.length < 10) {
      setError('Password must be at least 10 characters.');
      return;
    }
    setBusy(true);
    setError(undefined);
    try {
      await api.signup(email, password, name);
      selectProject(undefined);
      selectOrg(undefined);
      window.location.href = '/projects/new';
    } catch (err) {
      if (err instanceof APIError && err.status === 409) setError('An account with this email already exists.');
      else if (err instanceof APIError && err.status === 400) setError(err.message.charAt(0).toUpperCase() + err.message.slice(1) + '.');
      else setError('Could not create the account. Is the query API running?');
      setBusy(false);
    }
  };
  return (
    <AuthFrame>
      <Card className="w-full max-w-sm p-6 shadow-sm">
        <AuthHeading title="Create your Pulse account" subtitle="You get your own organization; add projects next." />
        <form onSubmit={submit} className="flex flex-col gap-3">
          <label className="flex flex-col gap-1 text-xs text-muted">
            Name
            <Input name="name" autoComplete="name" required maxLength={100} value={name} onChange={(e) => setName(e.target.value)} />
          </label>
          <label className="flex flex-col gap-1 text-xs text-muted">
            Email
            <Input type="email" name="email" autoComplete="username" required value={email} onChange={(e) => setEmail(e.target.value)} />
          </label>
          <label className="flex flex-col gap-1 text-xs text-muted">
            Password
            <Input
              type="password"
              name="password"
              autoComplete="new-password"
              required
              value={password}
              onChange={(e) => setPassword(e.target.value)}
            />
            <span className="text-[11px]">At least 10 characters.</span>
          </label>
          <ErrorNote error={error} />
          <Button type="submit" disabled={busy} className="mt-2">
            {busy ? 'Creating account…' : 'Create account'}
          </Button>
        </form>
        <p className="mt-4 text-xs text-muted">
          Already have an account?{' '}
          <Link href="/login" className="text-fg underline-offset-2 hover:underline">
            Sign in
          </Link>
        </p>
      </Card>
    </AuthFrame>
  );
}
