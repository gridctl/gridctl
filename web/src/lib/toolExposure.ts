// Count-line text for the Tools workspace list header and the sidebar editor.

export interface ToolExposureInput {
  selected: number;
  total: number;
  // Whether the server's saved allow-list is non-empty.
  hasAllowList: boolean;
  dirty: boolean;
}

export function toolExposureSummary({ selected, total, hasAllowList, dirty }: ToolExposureInput): string {
  if (dirty) return `${selected} of ${total} selected (unsaved)`;
  if (!hasAllowList) return `All ${total} ${total === 1 ? 'tool' : 'tools'} exposed (no allow-list)`;
  return `${selected} of ${total} exposed`;
}
