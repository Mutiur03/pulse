import { APIClient, APIError, type Me, type Org, type Project } from '@pulse/api-client';

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

const ORG_KEY = 'pulse.org';

export function storedOrg(): number | undefined {
  if (typeof window === 'undefined') return undefined;
  const v = Number(window.localStorage.getItem(ORG_KEY));
  return Number.isInteger(v) && v > 0 ? v : undefined;
}

export function selectOrg(id: number | undefined) {
  if (id === undefined) window.localStorage.removeItem(ORG_KEY);
  else window.localStorage.setItem(ORG_KEY, String(id));
}

export interface Context {
  org?: Org;
  /** Projects of the current organization. */
  projects: Project[];
  project?: Project;
}

/** The organization and project the user is looking at, from the switchers. */
export function currentContext(me: Me): Context {
  const sp = me.projects.find((p) => p.id === storedProject());
  const org = me.orgs.find((o) => o.id === storedOrg()) ?? me.orgs.find((o) => o.id === sp?.org_id) ?? me.orgs[0];
  const projects = org ? me.projects.filter((p) => p.org_id === org.id) : me.projects;
  const project = projects.find((p) => p.id === storedProject()) ?? projects[0];
  return { org, projects, project };
}

/** Sends the user to the login page when the session has expired. */
export function handleAuthError(e: unknown) {
  if (e instanceof APIError && e.status === 401 && typeof window !== 'undefined') {
    window.location.href = `/login?next=${encodeURIComponent(location.pathname + location.search)}`;
  }
}

/** The id of the project the user is looking at, once known. */
export async function currentProjectId(): Promise<number> {
  const stored = storedProject();
  if (stored !== undefined) return stored;
  const p = currentContext(await api.me()).project;
  if (!p) throw new Error('Create a project first.');
  return p.id;
}
