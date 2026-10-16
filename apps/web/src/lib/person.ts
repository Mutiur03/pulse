/** The name to show for a person: a trait if the app set one, else the id. */
export function displayName(p: { id: string; traits: Record<string, unknown> }): string {
  for (const k of ['name', 'email', 'username']) {
    const v = p.traits[k];
    if (typeof v === 'string' && v) return v;
  }
  return p.id;
}
