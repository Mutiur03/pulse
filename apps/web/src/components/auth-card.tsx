import { Card } from '@/components/ui';

/** Centered card used by the signed-out pages. */
export function AuthCard({ title, subtitle, children }: { title: string; subtitle?: string; children: React.ReactNode }) {
  return (
    <div className="grid min-h-screen place-items-center px-4">
      <Card className="w-full max-w-sm p-6">
        <div className="mb-6 flex items-center gap-2">
          <span className="grid h-8 w-8 place-items-center rounded-md bg-primary font-bold">P</span>
          <div>
            <h1 className="font-semibold">{title}</h1>
            {subtitle && <p className="text-xs text-muted">{subtitle}</p>}
          </div>
        </div>
        {children}
      </Card>
    </div>
  );
}
