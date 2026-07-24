import type { ReactNode } from 'react';
import { cn } from '../../lib/cn';

// Shared key/value table used by trace span detail and the log expand panel:
// promoted-field sections and raw attribute dumps render through the same
// shell so the two observability surfaces stay visually identical.
export function KVTable({ rows, monoKeys }: { rows: [string, ReactNode][]; monoKeys?: boolean }) {
  return (
    <div className="rounded-lg bg-surface-elevated/60 border border-border/30 overflow-hidden">
      <table className="w-full text-xs">
        <tbody>
          {rows.map(([key, value]) => (
            <tr key={key} className="border-b border-border/20 last:border-0 hover:bg-surface-highlight/20 transition-colors">
              <td className={cn('px-3 py-1.5 text-text-muted align-top w-[45%]', monoKeys && 'font-mono break-all')}>
                {key}
              </td>
              <td className="px-3 py-1.5 text-text-primary font-mono align-top break-all">{value}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
