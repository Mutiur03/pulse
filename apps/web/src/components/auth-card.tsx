import { Card } from '@/components/ui';
import { Logo } from '@/components/nav';

/** The page frame of the signed-out pages: the mark, a title and a card. */
export function AuthFrame({ children }: { children: React.ReactNode }) {
  return (
    <div className="soft-page soft-auth flex min-h-screen flex-col items-center justify-center px-4 py-10">
      <div className="mb-6 flex items-center gap-2">
        <Logo size={28} />
        <span className="text-[15px] font-semibold tracking-tight">Pulse</span>
      </div>
      {children}
      <p className="mt-6 text-center text-[11px] text-muted">Product analytics, traces, logs and errors in one place.</p>
    </div>
  );
}

export function AuthHeading({ title, subtitle }: { title: string; subtitle?: string }) {
  return (
    <div className="mb-5">
      <h1 className="text-base font-semibold tracking-tight">{title}</h1>
      {subtitle && <p className="mt-0.5 text-[13px] text-muted">{subtitle}</p>}
    </div>
  );
}

/** Centered card used by the signed-out pages. */
export function AuthCard({ title, subtitle, children }: { title: string; subtitle?: string; children: React.ReactNode }) {
  return (
    <AuthFrame>
      <Card className="w-full max-w-sm p-6 shadow-sm">
        <AuthHeading title={title} subtitle={subtitle} />
        {children}
      </Card>
    </AuthFrame>
  );
}
