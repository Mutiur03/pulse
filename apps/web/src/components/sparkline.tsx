export function Sparkline({ values, width = 120, height = 28, className }: { values: number[]; width?: number; height?: number; className?: string }) {
  const max = Math.max(1, ...values);
  const bw = width / Math.max(1, values.length);
  return (
    <svg width={width} height={height} className={className} role="img" aria-label="occurrences over the last 24 hours">
      {values.map((v, i) => {
        const h = v === 0 ? 1 : Math.max(2, (v / max) * (height - 2));
        return <rect key={i} x={i * bw + 0.5} y={height - h} width={Math.max(1, bw - 1.5)} height={h} rx={1} className={v === 0 ? 'fill-strong' : 'fill-primary'} />;
      })}
    </svg>
  );
}
