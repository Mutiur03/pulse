'use client';
import { use } from 'react';
import { useRouter } from 'next/navigation';
import { api, currentProjectId } from '@/lib/api';
import { useAsync } from '@/lib/hooks';
import { AlertForm, toInput } from '@/components/alert-form';
import { ErrorNote, PageHeader, Spinner } from '@/components/ui';

export default function EditAlertPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = use(params);
  const router = useRouter();
  const data = useAsync(async () => {
    const project = await currentProjectId();
    return { project, detail: await api.getAlert(project, Number(id)) };
  }, [id]);
  return (
    <>
      <PageHeader title={data.data ? `Edit ${data.data.detail.rule.name}` : 'Edit rule'} />
      <ErrorNote error={data.error} />
      {data.loading && <Spinner />}
      {data.data && (
        <AlertForm
          project={data.data.project}
          initial={toInput(data.data.detail.rule)}
          onSave={async (r) => {
            await api.updateAlert(data.data!.project, Number(id), r);
            router.push(`/alerts/${id}`);
          }}
        />
      )}
    </>
  );
}
