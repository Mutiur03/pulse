import { PulseClient } from './client';
import type { Properties, PulseConfig } from './config';

export type { PulseConfig, Properties, EventPayload, ErrorPayload } from './config';
export { PulseClient } from './client';

let client: PulseClient | undefined;

/** Start the SDK. Calling it again returns the existing client. */
export function init(config: PulseConfig): PulseClient {
  if (client) return client;
  if (typeof window === 'undefined') throw new Error('@pulse/browser must run in a browser');
  client = new PulseClient(config);
  client.start();
  return client;
}

export function capture(event: string, properties?: Properties): void {
  client?.capture(event, properties);
}

export function identify(userId: string, traits?: Properties): void {
  client?.identify(userId, traits);
}

/** Clear the identified user and session, e.g. on logout. */
/** Links another id to the current user; events under either id belong to one person. */
export function alias(id: string): void {
  client?.alias(id);
}

export function reset(): void {
  client?.reset();
}

export function captureException(err: unknown): void {
  client?.captureException(err);
}

export function flush(): Promise<void> {
  return client?.flush() ?? Promise.resolve();
}

export async function shutdown(): Promise<void> {
  const c = client;
  client = undefined;
  await c?.shutdown();
}

export function getClient(): PulseClient | undefined {
  return client;
}
