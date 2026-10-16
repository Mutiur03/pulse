'use client';
import { api, currentContext } from '@/lib/api';
import { useAsync } from '@/lib/hooks';
import { SetupSteps } from '@/components/onboarding';
import { Empty, ErrorNote, Loading } from '@/components/ui';

/** Setup instructions for the current project, reachable from empty states. */
export default function SetupPage() {
  const data = useAsync(async () => {
    const me = await api.me();
    const project = currentContext(me).project;
    if (!project) return undefined;
    const keys = await api.listKeys(project.id);
    return { project, key: keys.find((k) => !k.revoked_at)?.public_key };
  }, []);
  if (data.error) return <ErrorNote error={data.error} onRetry={data.reload} />;
  if (data.loading && !data.data) return <Loading rows={3} />;
  if (!data.data) return <Empty title="No project yet" action={{ label: 'Create a project', href: '/projects/new' }} />;
  const { project, key } = data.data;
  if (!key)
    return (
      <Empty title="No active ingest key" action={{ label: 'Open settings', href: '/settings' }}>
        Every key of {project.name} is revoked. Add one in Settings.
      </Empty>
    );
  return <SetupSteps name={project.name} project={project.id} apiKey={key} />;
}
