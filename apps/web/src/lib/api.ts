import { APIClient, APIError } from '@pulse/api-client';

export const api = new APIClient('/api');

/** Sends the user to the login page when the session has expired. */
export function handleAuthError(e: unknown) {
  if (e instanceof APIError && e.status === 401 && typeof window !== 'undefined') {
    window.location.href = `/login?next=${encodeURIComponent(location.pathname + location.search)}`;
  }
}
