import { ranges } from '@/lib/format';
import { Select } from './ui';

export function TimeRange({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  return (
    <Select aria-label="Time range" value={value} onChange={(e) => onChange(e.target.value)}>
      {Object.keys(ranges).map((r) => (
        <option key={r} value={r}>
          Last {r}
        </option>
      ))}
    </Select>
  );
}
