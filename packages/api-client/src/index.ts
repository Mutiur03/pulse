export interface Project {
  id: number;
  name: string;
  project_key: string;
}

export interface Event {
  event: string;
  timestamp: string;
  properties?: Record<string, any>;
  trace_id?: string;
}

export interface Span {
  span_id: string;
  name: string;
  timestamp: string;
  duration_ns?: number;
}

export interface Trace {
  trace_id: string;
  spans: Span[];
}

export class APIClient {
  private baseURL: string;

  constructor(baseURL: string) {
    this.baseURL = baseURL;
  }

  async getHealth(): Promise<{ status: string; service: string }> {
    const response = await fetch(`${this.baseURL}/health`);
    return response.json();
  }

  async listProjects(): Promise<{ projects: Project[] }> {
    const response = await fetch(`${this.baseURL}/api/v1/projects`);
    return response.json();
  }

  async queryEvents(params?: {
    project_id?: number;
    start_time?: string;
    end_time?: string;
  }): Promise<{ events: Event[] }> {
    const query = new URLSearchParams();
    if (params?.project_id) query.set('project_id', params.project_id.toString());
    if (params?.start_time) query.set('start_time', params.start_time);
    if (params?.end_time) query.set('end_time', params.end_time);

    const response = await fetch(`${this.baseURL}/api/v1/events?${query}`);
    return response.json();
  }

  async getTrace(traceId: string): Promise<Trace> {
    const response = await fetch(`${this.baseURL}/api/v1/traces/${traceId}`);
    return response.json();
  }
}
