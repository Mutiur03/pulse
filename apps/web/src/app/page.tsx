export default function Home() {
  return (
    <main className="flex min-h-screen flex-col items-center justify-center p-24">
      <div className="z-10 max-w-5xl w-full items-center justify-between font-mono text-sm">
        <h1 className="text-4xl font-bold mb-8">
          Telemetry Platform
        </h1>
        <p className="text-lg mb-4">
          Unified product analytics and observability platform
        </p>
        <div className="grid grid-cols-1 md:grid-cols-3 gap-4 mt-8">
          <div className="border p-4 rounded">
            <h2 className="font-bold mb-2">Product Analytics</h2>
            <p className="text-sm text-gray-600">
              Events, funnels, retention, cohorts
            </p>
          </div>
          <div className="border p-4 rounded">
            <h2 className="font-bold mb-2">Observability</h2>
            <p className="text-sm text-gray-600">
              Traces, logs, metrics, dashboards
            </p>
          </div>
          <div className="border p-4 rounded">
            <h2 className="font-bold mb-2">Error Tracking</h2>
            <p className="text-sm text-gray-600">
              Issues, stack traces, releases
            </p>
          </div>
        </div>
        <div className="mt-8">
          <a 
            href="/api/health" 
            className="text-blue-600 hover:underline"
            target="_blank"
          >
            Check API Health →
          </a>
        </div>
      </div>
    </main>
  );
}
