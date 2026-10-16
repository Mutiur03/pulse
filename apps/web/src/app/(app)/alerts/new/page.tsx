'use client';
import { Suspense } from 'react';
import { useRouter, useSearchParams } from 'next/navigation';
import type { AlertRuleInput } from '@pulse/api-client';
import { api, currentProjectId } from '@/lib/api';
import { useAsync } from '@/lib/hooks';
import { AlertForm } from '@/components/alert-form';
import { ErrorNote, PageHeader, Spinner } from '@/components/ui';

export default function NewAlertPage() {
  return (
    <Suspense fallback={<Spinner />}>
      <NewAlert />
    </Suspense>
  );
}

// The logs explorer links here with ?log_query= to alert on a search.
function NewAlert() {
  const router = useRouter();
  const params = useSearchParams();
  const logQuery = params.get('log_query');
  const initial: Partial<AlertRuleInput> | undefined =
    logQuery !== null ? { metric: 'log_count', log_query: logQuery, threshold: 10, name: `Logs: ${logQuery || 'all'}`.slice(0, 200) } : undefined;
  const project = useAsync(() => currentProjectId(), []);
  return (
    <>
      <PageHeader title="New alert rule" />
      <ErrorNote error={project.error} />
      {project.loading && <Spinner />}
      {project.data !== undefined && (
        <AlertForm
          project={project.data}
          initial={initial}
          onSave={async (r) => {
            const created = await api.createAlert(project.data!, r);
            router.push(`/alerts/${created.id}`);
          }}
        />
      )}
    </>
  );
}
