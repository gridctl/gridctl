// Shared clamps for the four text-size preferences. The inline zoom hooks and
// the Preferences dialog both read these so a row cannot drift from its pane.

export interface TextZoomSpec {
  name: string;
  label: string;
  defaultSize: number;
  minSize: number;
  maxSize: number;
  step: number;
}

export const LIBRARY_TEXT_ZOOM: TextZoomSpec = {
  name: 'library-zoom',
  label: 'Library',
  defaultSize: 13,
  minSize: 8,
  maxSize: 22,
  step: 1,
};

export const TOOLS_TEXT_ZOOM: TextZoomSpec = {
  name: 'tools-zoom',
  label: 'Tools',
  defaultSize: 12,
  minSize: 8,
  maxSize: 22,
  step: 1,
};

export const TRACES_TEXT_ZOOM: TextZoomSpec = {
  name: 'traces-font-size',
  label: 'Traces',
  defaultSize: 11,
  minSize: 8,
  maxSize: 20,
  step: 1,
};

export const LOG_TEXT_ZOOM: TextZoomSpec = {
  name: 'log-font-size',
  label: 'Log font size',
  defaultSize: 11,
  minSize: 8,
  maxSize: 22,
  step: 1,
};

export const TEXT_ZOOM_ROWS: readonly TextZoomSpec[] = [
  LIBRARY_TEXT_ZOOM,
  TOOLS_TEXT_ZOOM,
  TRACES_TEXT_ZOOM,
  LOG_TEXT_ZOOM,
];
