'use client';
import Link from 'next/link';
import { Suspense, useEffect, useRef, useState } from 'react';
import { useSearchParams } from 'next/navigation';
import { api } from '@/lib/api';
import { AuthCard } from '@/components/auth-card';
import { Spinner } from '@/components/ui';

function Verify() {
  const token = useSearchParams().get('token') ?? '';
  const [state, setState] = useState<'busy' | 'ok' | 'error'>('busy');
  const once = useRef(false);
  useEffect(() => {
    if (once.current) return;
    once.current = true;
    api.verifyEmail(token).then(
      () => setState('ok'),
      () => setState('error'),
    );
  }, [token]);
  return (
    <AuthCard title="Confirm your email">
      <div className="text-sm text-fg-2" data-testid="verify-result">
        {state === 'busy' && <Spinner />}
        {state === 'ok' && 'Your email address is confirmed.'}
        {state === 'error' && 'This link is invalid or has expired. Sign in and use "Resend email" to get a new one.'}
      </div>
      <p className="mt-4 text-xs">
        <Link href="/traces" className="text-fg underline-offset-2 hover:underline">
          Continue to Pulse
        </Link>
      </p>
    </AuthCard>
  );
}

export default function VerifyEmailPage() {
  return (
    <Suspense>
      <Verify />
    </Suspense>
  );
}
