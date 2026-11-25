export type ContextDriftCase = 'single-file' | 'multi-file-identity' | 'multi-file-lossy' | 'compiled';

// contextDriftCliHints names the CLI equivalents of the actions a context
// drift dialog offers in each case. Each command mirrors a button the dialog
// shows there: the engine refuses a bare adopt on multi-file and compiled
// clients and refuses adopt entirely on a lossy render, so naming one would
// hand the operator a command that fails.
export function contextDriftCliHints(slug: string, kind: ContextDriftCase, fragment?: string | null): string[] {
  const frag = fragment || '<fragment>';
  switch (kind) {
    case 'single-file':
      return [`gridctl ctx diff ${slug}`, `gridctl ctx adopt ${slug}`, `gridctl ctx sync ${slug} --force`];
    case 'multi-file-identity':
      return [
        `gridctl ctx diff ${slug} ${frag}`,
        `gridctl ctx adopt ${slug} ${frag}`,
        `gridctl ctx sync ${slug} --force`,
      ];
    case 'multi-file-lossy':
      return [`gridctl ctx diff ${slug} ${frag}`, `gridctl ctx sync ${slug} --force`];
    case 'compiled':
      return [
        `gridctl ctx diff ${slug}`,
        `gridctl ctx adopt ${slug} --into <fragment>`,
        `gridctl ctx sync ${slug} --force`,
      ];
  }
}
