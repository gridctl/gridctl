import { useEffect, useRef, useState } from 'react';

interface UsePageFileDropOptions {
  // When false the listeners are torn down entirely — used to suppress the
  // page dropzone while a modal is open or the surface is otherwise busy.
  enabled: boolean;
  // Called once per drop with the dropped FileList (when it contains files).
  onFiles: (files: FileList) => void;
}

// usePageFileDrop wires window-level drag listeners so a file dragged anywhere
// over the page reveals a dropzone. It only reacts to drags carrying files
// (so internal pointer drags — panel resizers, canvas nodes — are ignored) and
// uses a depth counter to avoid the classic dragenter/dragleave flicker as the
// cursor crosses child elements. Native, no dependency.
export function usePageFileDrop({ enabled, onFiles }: UsePageFileDropOptions) {
  const [isDragging, setIsDragging] = useState(false);
  const depth = useRef(0);
  // Hold the latest callback in a ref so changing it doesn't re-subscribe.
  const onFilesRef = useRef(onFiles);
  useEffect(() => {
    onFilesRef.current = onFiles;
  }, [onFiles]);

  useEffect(() => {
    if (!enabled) {
      depth.current = 0;
      return;
    }

    const hasFiles = (e: DragEvent) =>
      !!e.dataTransfer && Array.from(e.dataTransfer.types).includes('Files');

    const onDragEnter = (e: DragEvent) => {
      if (!hasFiles(e)) return;
      e.preventDefault();
      depth.current += 1;
      setIsDragging(true);
    };

    const onDragOver = (e: DragEvent) => {
      // preventDefault is required on dragover or the drop never fires and the
      // browser navigates away to open the file.
      if (!hasFiles(e)) return;
      e.preventDefault();
    };

    const reset = () => {
      depth.current = 0;
      setIsDragging(false);
    };

    const onDragLeave = (e: DragEvent) => {
      if (!hasFiles(e)) return;
      // A null relatedTarget means the cursor left the window entirely — hard
      // reset rather than decrementing, since the matching enters won't come.
      if (e.relatedTarget === null) {
        reset();
        return;
      }
      depth.current = Math.max(0, depth.current - 1);
      if (depth.current === 0) setIsDragging(false);
    };

    const onDrop = (e: DragEvent) => {
      if (!hasFiles(e)) return;
      e.preventDefault();
      reset();
      const files = e.dataTransfer?.files;
      if (files && files.length > 0) onFilesRef.current(files);
    };

    window.addEventListener('dragenter', onDragEnter);
    window.addEventListener('dragover', onDragOver);
    window.addEventListener('dragleave', onDragLeave);
    window.addEventListener('drop', onDrop);
    // A cancelled drag (Escape, or dropping outside the window) fires neither a
    // drop nor a balancing dragleave, so without this the overlay would stick.
    window.addEventListener('dragend', reset);
    return () => {
      window.removeEventListener('dragenter', onDragEnter);
      window.removeEventListener('dragover', onDragOver);
      window.removeEventListener('dragleave', onDragLeave);
      window.removeEventListener('drop', onDrop);
      window.removeEventListener('dragend', reset);
      depth.current = 0;
    };
  }, [enabled]);

  // Mask any residual dragging state while disabled so callers never see a
  // stale overlay when the surface is suppressed.
  return { isDragging: enabled && isDragging };
}
