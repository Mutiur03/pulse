'use client';
import { useEffect, useState } from 'react';
import type { RetentionSettings, ScrubRules } from '@pulse/api-client';
import { api, currentContext, handleAuthError } from './api';
import { useAsync } from './hooks';

const lines = (v: string) =>
  v
    .split('\n')
    .map((x) => x.trim())
    .filter(Boolean);

/**
 * State and actions of the data governance settings page: the project's
 * scrubbing and retention settings as an editable form, the scrubbing
 * preview, and deletion requests (polled while any is still running).
 */
export function useDataGovernance() {
  const me = useAsync(() => api.me(), []);
  const project = me.data ? currentContext(me.data).project : undefined;
  const owner = project?.role === 'owner';
  const settings = useAsync(() => (project ? api.getDataSettings(project.id) : Promise.resolve(undefined)), [project?.id]);
  const requests = useAsync(() => (project ? api.listDeletionRequests(project.id) : Promise.resolve([])), [project?.id]);
  const [rules, setRules] = useState<ScrubRules>();
  const [keys, setKeys] = useState('');
  const [patterns, setPatterns] = useState('');
  const [retention, setRetention] = useState<RetentionSettings>();
  const [sample, setSample] = useState('{"email": "ana@example.com", "card": "4242 4242 4242 4242", "ip": "203.0.113.9", "password": "hunter2"}');
  const [preview, setPreview] = useState<string>();
  const [person, setPerson] = useState('');
  const [reason, setReason] = useState('');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string>();
  const [saved, setSaved] = useState(false);

  useEffect(() => {
    const s = settings.data;
    if (!s) return;
    setRules(s.scrubbing);
    setKeys(s.scrubbing.keys.join('\n'));
    setPatterns(s.scrubbing.patterns.join('\n'));
    setRetention(s.retention);
  }, [settings.data]);

  // Poll while a request is still running.
  const open = requests.data?.some((r) => r.status === 'pending' || r.status === 'running');
  useEffect(() => {
    if (!open) return;
    const t = setInterval(requests.reload, 2000);
    return () => clearInterval(t);
  }, [open, requests.reload]);

  const current = (): ScrubRules | undefined => rules && { ...rules, keys: lines(keys), patterns: lines(patterns) };

  const act = async (fn: () => Promise<unknown>) => {
    setBusy(true);
    setErr(undefined);
    setSaved(false);
    try {
      await fn();
    } catch (e) {
      handleAuthError(e);
      setErr(e instanceof Error ? e.message : String(e));
    }
    setBusy(false);
  };

  const save = (e: React.FormEvent) => {
    e.preventDefault();
    const scrubbing = current();
    if (!project || !scrubbing || !retention) return;
    void act(async () => {
      await api.updateDataSettings(project.id, { scrubbing, retention });
      settings.reload();
      setSaved(true);
    });
  };
  const runPreview = () => {
    const scrubbing = current();
    if (!project || !scrubbing) return;
    void act(async () => setPreview((await api.previewScrubbing(project.id, scrubbing, sample)).scrubbed));
  };
  const requestDeletion = (e: React.FormEvent) => {
    e.preventDefault();
    const id = person.trim();
    if (!project || !id) return;
    if (!confirm(`Delete all data of ${id} and every id linked to it? This cannot be undone.`)) return;
    void act(async () => {
      await api.createDeletionRequest(project.id, id, reason.trim() || undefined);
      setPerson('');
      setReason('');
      requests.reload();
    });
  };

  return { me, project, owner, settings, requests, rules, setRules, keys, setKeys, patterns, setPatterns, retention, setRetention, sample, setSample, preview, person, setPerson, reason, setReason, busy, err, saved, save, runPreview, requestDeletion };
}
