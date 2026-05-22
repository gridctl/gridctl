import { KeyRound } from 'lucide-react';
import { cn } from '../../lib/cn';

export interface EmptyVaultStateProps {
  // The CLI verb to show in the example commands. VaultPanel renders the
  // current verb ("var"); the detached page historically rendered "vault"
  // (now a stale alias) — kept as a prop to preserve behavioral parity
  // until Phases 2-3 clean up.
  cliVerb: string;
  className?: string;
}

export function EmptyVaultState({ cliVerb, className }: EmptyVaultStateProps) {
  return (
    <div className={cn('px-4 py-8 text-center', className)}>
      <div className="mx-auto w-12 h-12 mb-4 rounded-xl bg-primary/10 border border-primary/20 flex items-center justify-center">
        <KeyRound size={20} className="text-primary/60" />
      </div>
      <p className="text-sm text-text-secondary mb-2">No secrets stored</p>
      <p className="text-xs text-text-muted leading-relaxed">
        Add secrets using the form above, or via CLI:
      </p>
      <div className="mt-2 space-y-1">
        <code className="block text-[10px] font-mono text-primary/80 bg-surface-elevated rounded px-2 py-1">
          gridctl {cliVerb} set API_KEY
        </code>
        <code className="block text-[10px] font-mono text-primary/80 bg-surface-elevated rounded px-2 py-1">
          gridctl {cliVerb} import .env
        </code>
      </div>
    </div>
  );
}
