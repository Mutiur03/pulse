'use client';
import { useRouter } from 'next/navigation';
import { api, currentProjectId } from '@/lib/api';
import { useAsync } from '@/lib/hooks';
import { AlertForm } from '@/components/alert-form';
import { ErrorNote, PageHeader, Spinner } from '@/components/ui';

export default function NewAlertPage() {
  const router = useRouter();
  const project = useAsync(() => currentProjectId(), []);
  return (
    <>
      <PageHeader title="New alert rule" />
      <ErrorNote error={project.error} />
      {project.loading && <Spinner />}
      {project.data !== undefined && (
        <AlertForm
          project={project.data}
          onSave={async (r) => {
            const created = await api.createAlert(project.data!, r);
            router.push(`/alerts/${created.id}`);
          }}
        />
      )}
    </>
  );
}
