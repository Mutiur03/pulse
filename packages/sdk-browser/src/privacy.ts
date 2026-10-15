/**
 * Describes a clicked element for analytics without ever reading what the
 * user typed: no .value, no text from form fields, no password managers.
 */
const FORM_TAGS = new Set(['INPUT', 'TEXTAREA', 'SELECT', 'OPTION']);
const MAX_TEXT = 64;

export function isSensitive(el: Element): boolean {
  if (FORM_TAGS.has(el.tagName)) return true;
  // Checked by attribute as well, since isContentEditable is not available everywhere.
  if ((el as HTMLElement).isContentEditable) return true;
  return !!el.closest('[contenteditable]:not([contenteditable="false"]), [data-pulse-no-capture], .pulse-no-capture');
}

/** Visible text of an element, skipping any form fields or opted-out nodes inside it. */
export function safeText(el: Element): string {
  if (isSensitive(el)) return '';
  let out = '';
  const walk = (node: Node) => {
    if (out.length > MAX_TEXT) return;
    if (node.nodeType === 3) out += node.textContent ?? '';
    else if (node.nodeType === 1 && !isSensitive(node as Element)) node.childNodes.forEach(walk);
  };
  el.childNodes.forEach(walk);
  return out.replace(/\s+/g, ' ').trim().slice(0, MAX_TEXT);
}

export function selector(el: Element): string {
  const parts: string[] = [];
  let cur: Element | null = el;
  for (let i = 0; cur && i < 5 && cur.tagName !== 'HTML'; i++) {
    let p = cur.tagName.toLowerCase();
    if (cur.id) {
      parts.unshift(`${p}#${cur.id}`);
      break;
    }
    const cls = Array.from(cur.classList).slice(0, 2);
    if (cls.length) p += '.' + cls.join('.');
    parts.unshift(p);
    cur = cur.parentElement;
  }
  return parts.join(' > ');
}

/** Strip query string and hash, which often carry tokens or emails. */
export function safeUrl(href: string): string {
  try {
    const u = new URL(href, globalThis.location?.href);
    return u.origin + u.pathname;
  } catch {
    return '';
  }
}

export function describeElement(target: Element): Record<string, unknown> {
  const el = (target.closest('a,button,[role=button],[data-pulse-event]') ?? target) as Element;
  const props: Record<string, unknown> = {
    $tag: el.tagName.toLowerCase(),
    $selector: selector(el),
  };
  if (el.id) props.$id = el.id;
  const name = el.getAttribute('data-pulse-event');
  if (name) props.$name = name;
  const text = safeText(el);
  if (text) props.$text = text;
  if (el.tagName === 'A') {
    const href = el.getAttribute('href');
    if (href) props.$href = safeUrl(href);
  }
  if (FORM_TAGS.has(el.tagName)) {
    // Only the field's type and name, never its value.
    const type = el.getAttribute('type');
    if (type) props.$input_type = type;
    const n = el.getAttribute('name');
    if (n) props.$input_name = n;
  }
  return props;
}
