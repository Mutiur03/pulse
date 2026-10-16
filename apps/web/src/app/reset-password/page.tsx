'use client';
import Link from 'next/link';
import { Suspense, useState } from 'react';
import { useSearchParams } from 'next/navigation';
import { APIError } from '@pulse/api-client';
import { api } from '@/lib/api';
import { AuthCard } from '@/components/auth-card';
import { Button, ErrorNote, Input } from '@/components/ui';

const back = (
  <p className="mt-4 text-xs text-muted">
    <Link href="/login" className="text-fg underline-offset-2 hover:underline">
      Back to sign in
    </Link>
  </p>
);

function RequestForm() {
  const [email, setEmail] = useState('');
  const [busy, setBusy] = useState(false);
  const [sent, setSent] = useState(false);
  const [error, setError] = useState<string>();
  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError(undefined);
    try {
      await api.requestPasswordReset(email.trim());
      setSent(true);
    } catch (err) {
      setError(err instanceof APIError && err.status === 429 ? 'Too many requests. Try again later.' : 'Could not send the email. Try again.');
    }
    setBusy(false);
  };
  return (
    <AuthCard title="Reset your password" subtitle="We will email you a link that works for one hour.">
      {sent ? (
        <p className="text-sm text-fg-2" data-testid="reset-sent">
          If an account exists for <span className="font-medium">{email.trim()}</span>, a reset link is on its way. Check your inbox.
        </p>
      ) : (
        <form onSubmit={submit} className="flex flex-col gap-3">
          <label className="flex flex-col gap-1 text-xs text-muted">
            Email
            <Input type="email" name="email" autoComplete="username" required value={email} onChange={(e) => setEmail(e.target.value)} />
          </label>
          <ErrorNote error={error} />
          <Button type="submit" disabled={busy} className="mt-2">
            {busy ? 'Sending…' : 'Send reset link'}
          </Button>
        </form>
      )}
      {back}
    </AuthCard>
  );
}

function ConfirmForm({ token }: { token: string }) {
  const [password, setPassword] = useState('');
  const [busy, setBusy] = useState(false);
  const [done, setDone] = useState(false);
  const [error, setError] = useState<string>();
  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (password.length < 10) {
      setError('Password must be at least 10 characters.');
      return;
    }
    setBusy(true);
    setError(undefined);
    try {
      await api.confirmPasswordReset(token, password);
      setDone(true);
    } catch (err) {
      setError(err instanceof APIError && err.status === 400 ? err.message.charAt(0).toUpperCase() + err.message.slice(1) + '.' : 'Could not reset the password.');
    }
    setBusy(false);
  };
  return (
    <AuthCard title="Choose a new password" subtitle="Every device signed in to your account will be signed out.">
      {done ? (
        <div className="flex flex-col gap-3 text-sm text-fg-2" data-testid="reset-done">
          Your password was changed.
          <Button onClick={() => (window.location.href = '/login')}>Sign in</Button>
        </div>
      ) : (
        <form onSubmit={submit} className="flex flex-col gap-3">
          <label className="flex flex-col gap-1 text-xs text-muted">
            New password
            <Input type="password" name="password" autoComplete="new-password" required value={password} onChange={(e) => setPassword(e.target.value)} />
            <span className="text-[11px]">At least 10 characters.</span>
          </label>
          <ErrorNote error={error} />
          <Button type="submit" disabled={busy} className="mt-2">
            {busy ? 'Saving…' : 'Set new password'}
          </Button>
        </form>
      )}
      {!done && back}
    </AuthCard>
  );
}

function ResetPassword() {
  const token = useSearchParams().get('token');
  return token ? <ConfirmForm token={token} /> : <RequestForm />;
}

export default function ResetPasswordPage() {
  return (
    <Suspense>
      <ResetPassword />
    </Suspense>
  );
}
