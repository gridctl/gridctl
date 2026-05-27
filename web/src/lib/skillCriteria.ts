// Parsing for the gridctl "GIVEN … WHEN … THEN …" acceptance-criteria strings
// carried on AgentSkill.acceptanceCriteria. Shared by the SkillEditor (which
// splits criteria into editable fields) and the Library inspector (which
// renders them read-only).

export interface ParsedCriterion {
  given: string;
  when: string;
  then: string;
  /** True when the string matched the GIVEN/WHEN/THEN shape. */
  matched: boolean;
  /** The original, unparsed string. */
  raw: string;
}

const CRITERION_RE = /GIVEN\s+(.+?)\s+WHEN\s+(.+?)\s+THEN\s+(.+)/i;

/**
 * Split a "GIVEN … WHEN … THEN …" criterion into its three parts. When the
 * string doesn't match the shape, `given` falls back to the raw string (so the
 * editor still shows the text) and `matched` is false.
 */
export function parseAcceptanceCriterion(raw: string): ParsedCriterion {
  const m = raw.match(CRITERION_RE);
  if (m) {
    return { given: m[1], when: m[2], then: m[3], matched: true, raw };
  }
  return { given: raw, when: '', then: '', matched: false, raw };
}
