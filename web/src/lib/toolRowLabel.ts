// Accessible name for a Tools workspace list row. role="option" has
// presentational children, so this label is the only announcement of the
// row's state and chips. Pure and JSX-free.
import { ANNOTATION_CHIP_LEGEND, annotationChips } from './groups';
import type { AuditState } from './toolAudit';
import type { ToolAnnotations } from '../types';

const CHIP_NAME = new Map(ANNOTATION_CHIP_LEGEND.map((e) => [e.label, e.name.toLowerCase()]));

export interface ToolRowLabelInput {
  name: string;
  isEnabled: boolean;
  groups: readonly string[];
  annotations?: ToolAnnotations;
  /** policyDetailText for the row's verdict; empty when there is no chip. */
  policyText: string;
  auditState: AuditState | null;
  lastUsed: string;
}

export function toolRowLabel(input: ToolRowLabelInput): string {
  const parts: string[] = [input.name, input.isEnabled ? 'enabled' : 'disabled'];
  for (const g of input.groups) parts.push(`group ${g}`);
  for (const chip of annotationChips(input.annotations)) {
    parts.push(CHIP_NAME.get(chip.label) ?? chip.label);
  }
  if (input.policyText) parts.push(input.policyText);
  // A disabled audit state repeats the enabled state, so it adds nothing.
  if (input.auditState && input.auditState !== 'disabled') {
    parts.push(input.auditState);
    if (input.lastUsed) parts.push(`last used ${input.lastUsed}`);
  }
  return parts.join(', ');
}
