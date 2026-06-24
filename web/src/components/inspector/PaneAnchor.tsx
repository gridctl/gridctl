/**
 * PaneAnchor draws a thin amber bar down the leading (left) edge of a
 * detail/inspector pane. It rhymes with the active list item's own left accent
 * (e.g. SkillCard), forming a visual breadcrumb from the selected object to its
 * expanded properties. Purely decorative — the owning pane must be `relative`.
 */
export function PaneAnchor() {
  return (
    <span
      aria-hidden="true"
      className="pointer-events-none absolute inset-y-0 left-0 w-0.5 bg-primary/40"
    />
  );
}
