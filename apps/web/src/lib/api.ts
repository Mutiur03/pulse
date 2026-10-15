import { APIClient, APIError } from '@pulse/api-client';

export const api = new APIClient('/api');

const PROJECT_KEY = 'pulse.project';

/** The project picked in the switcher, if any. */
export function storedProject(): number | undefined {
  if (typeof window === 'undefined') return undefined;
  const v = Number(window.localStorage.getItem(PROJECT_KEY));
  return Number.isInteger(v) && v > 0 ? v : undefined;
}

export function selectProject(id: number | undefined) {
  if (id === undefined) window.localStorage.removeItem(PROJECT_KEY);
  else window.localStorage.setItem(PROJECT_KEY, String(id));
  api.project = id;
}

api.project = storedProject();

/** Sends the user to the login page when the session has expired. */
export function handleAuthError(e: unknown) {
  if (e instanceof APIError && e.status === 401 && typeof window !== 'undefined') {
    window.location.href = `/login?next=${encodeURIComponent(location.pathname + location.search)}`;
  }
}
