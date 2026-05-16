import { useMemo } from 'react';
import { cn } from '../../lib/cn';
import { tokenize, type CodeLanguage } from './tokenize';

interface CodeViewerProps {
  content: string;
  language: CodeLanguage;
  toolbarSlot?: React.ReactNode;
  fontSize?: number;
  wrap?: boolean;
  ariaLabel?: string;
  className?: string;
}

export function CodeViewer({
  content,
  language,
  toolbarSlot,
  fontSize,
  wrap = false,
  ariaLabel,
  className,
}: CodeViewerProps) {
  const lines = useMemo(() => tokenize(content, language), [content, language]);
  const tableStyle = fontSize != null ? { fontSize: `${fontSize}px` } : undefined;

  return (
    <div className={cn('flex flex-col min-h-0', className)}>
      {toolbarSlot}
      <div
        role="region"
        aria-label={ariaLabel}
        className="flex-1 min-h-0 overflow-auto scrollbar-dark font-mono text-xs leading-relaxed"
      >
        <table className="w-full border-collapse" style={tableStyle}>
          <tbody>
            {lines.map((tokens, idx) => {
              const lineNum = idx + 1;
              return (
                <tr key={lineNum} className="group">
                  <td className="w-12 pr-3 text-right text-text-muted/40 select-none align-top py-px">
                    {lineNum}
                  </td>
                  <td
                    className={cn(
                      'pr-4 py-px align-top',
                      wrap ? 'whitespace-pre-wrap break-words' : 'whitespace-pre',
                    )}
                  >
                    {tokens.length === 0 ? (
                      <span>{'​'}</span>
                    ) : (
                      tokens.map((token, i) => (
                        <span key={i} className={token.className}>
                          {token.text}
                        </span>
                      ))
                    )}
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </div>
  );
}
