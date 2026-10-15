import { v4 as uuidv4 } from 'uuid';

export interface SDKConfig {
  projectKey: string;
  endpoint: string;
  release?: string;
  autocapture?: boolean;
  propagateTraceHeaderCorsUrls?: (string | RegExp)[];
}

export interface EventProperties {
  [key: string]: any;
}

export class TelemetrySDK {
  private config: SDKConfig;
  private sessionId: string;
  private distinctId: string;

  constructor(config: SDKConfig) {
    this.config = config;
    this.sessionId = this.getOrCreateSessionId();
    this.distinctId = this.getOrCreateDistinctId();

    if (config.autocapture) {
      this.setupAutocapture();
    }

    if (config.propagateTraceHeaderCorsUrls) {
      this.setupFetchInterception();
    }
  }

  private getOrCreateSessionId(): string {
    const key = '_telemetry_session_id';
    let sessionId = sessionStorage.getItem(key);
    if (!sessionId) {
      sessionId = uuidv4();
      sessionStorage.setItem(key, sessionId);
    }
    return sessionId;
  }

  private getOrCreateDistinctId(): string {
    const key = '_telemetry_distinct_id';
    let distinctId = localStorage.getItem(key);
    if (!distinctId) {
      distinctId = uuidv4();
      localStorage.setItem(key, distinctId);
    }
    return distinctId;
  }

  private generateTraceId(): string {
    return Array.from({ length: 32 }, () =>
      Math.floor(Math.random() * 16).toString(16)
    ).join('');
  }

  private generateSpanId(): string {
    return Array.from({ length: 16 }, () =>
      Math.floor(Math.random() * 16).toString(16)
    ).join('');
  }

  private generateTraceparent(traceId?: string): string {
    const tid = traceId || this.generateTraceId();
    const spanId = this.generateSpanId();
    return `00-${tid}-${spanId}-01`;
  }

  private setupFetchInterception(): void {
    if (typeof window === 'undefined') return;

    const originalFetch = window.fetch;
    const sdk = this;

    window.fetch = function (input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
      const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
      
      const shouldPropagate = sdk.config.propagateTraceHeaderCorsUrls?.some(pattern => {
        if (typeof pattern === 'string') {
          return url.includes(pattern);
        }
        return pattern.test(url);
      });

      if (shouldPropagate) {
        const traceId = sdk.generateTraceId();
        const traceparent = sdk.generateTraceparent(traceId);
        
        const headers = new Headers(init?.headers);
        headers.set('traceparent', traceparent);
        headers.set('baggage', `session.id=${sdk.sessionId}`);

        init = {
          ...init,
          headers,
        };

        sdk.capture('$fetch', {
          $url: url,
          $trace_id: traceId,
        });
      }

      return originalFetch.call(this, input, init);
    };
  }

  public capture(event: string, properties?: EventProperties): void {
    const traceId = this.generateTraceId();
    const eventData = {
      project_id: this.config.projectKey,
      event,
      timestamp: new Date().toISOString(),
      distinct_id: this.distinctId,
      session_id: this.sessionId,
      trace_id: traceId,
      properties: {
        ...properties,
        $current_url: window.location.href,
        $browser: navigator.userAgent,
        ...(this.config.release && { release: this.config.release }),
      },
    };

    this.send('/v1/events', eventData);
  }

  public identify(userId: string, traits?: Record<string, any>): void {
    this.capture('$identify', {
      $user_id: userId,
      $anon_distinct_id: this.distinctId,
      ...traits,
    });
  }

  private setupAutocapture(): void {
    if (typeof window === 'undefined') return;

    document.addEventListener('click', (e) => {
      const target = e.target as HTMLElement;
      const tagName = target.tagName.toLowerCase();
      const text = target.textContent?.slice(0, 100) || '';

      this.capture('$autocapture', {
        $event_type: 'click',
        $element_tag: tagName,
        $element_text: text,
      });
    });

    this.capture('$pageview', {
      $pathname: window.location.pathname,
      $search: window.location.search,
    });
  }

  private send(path: string, data: any): void {
    const url = `${this.config.endpoint}${path}`;

    if (navigator.sendBeacon) {
      navigator.sendBeacon(url, JSON.stringify(data));
    } else {
      fetch(url, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify(data),
        keepalive: true,
      }).catch((error) => {
        console.error('Failed to send telemetry:', error);
      });
    }
  }
}

export function init(config: SDKConfig): TelemetrySDK {
  return new TelemetrySDK(config);
}
