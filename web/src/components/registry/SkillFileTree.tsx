import { useState, useEffect } from 'react';
import {
  File,
  FolderOpen,
  Folder,
  Plus,
  Trash2,
  ChevronDown,
  ChevronRight,
} from 'lucide-react';
import { fetchSkillFiles, writeSkillFile, deleteSkillFile } from '../../lib/api';
import { showToast } from '../ui/Toast';
import type { SkillFile } from '../../types';

interface SkillFileTreeProps {
  skillName: string;
  onSelectFile?: (path: string, content: string) => void;
}

export function SkillFileTree({ skillName, onSelectFile }: SkillFileTreeProps) {
  const [files, setFiles] = useState<SkillFile[]>([]);
  const [loading, setLoading] = useState(false);
  const [expandedDirs, setExpandedDirs] = useState<Set<string>>(new Set(['scripts', 'references', 'assets']));
  const [showNewFile, setShowNewFile] = useState(false);
  const [newFilePath, setNewFilePath] = useState('');

  // Fetch files on mount and when skill changes
  useEffect(() => {
    if (!skillName) return;
    setLoading(true);
    fetchSkillFiles(skillName)
      .then(setFiles)
      .catch(() => setFiles([]))
      .finally(() => setLoading(false));
  }, [skillName]);

  const toggleDir = (dir: string) => {
    setExpandedDirs((prev) => {
      const next = new Set(prev);
      if (next.has(dir)) next.delete(dir);
      else next.add(dir);
      return next;
    });
  };

  const handleCreateFile = async () => {
    if (!newFilePath.trim()) return;
    try {
      await writeSkillFile(skillName, newFilePath.trim(), '');
      showToast('success', `File created: ${newFilePath}`);
      setNewFilePath('');
      setShowNewFile(false);
      const updated = await fetchSkillFiles(skillName);
      setFiles(updated);
    } catch (err) {
      showToast('error', err instanceof Error ? err.message : 'Failed to create file');
    }
  };

  const handleDeleteFile = async (path: string) => {
    try {
      await deleteSkillFile(skillName, path);
      showToast('success', `File deleted: ${path}`);
      const updated = await fetchSkillFiles(skillName);
      setFiles(updated);
    } catch (err) {
      showToast('error', err instanceof Error ? err.message : 'Failed to delete file');
    }
  };

  const grouped = groupFilesByDir(files);

  if (loading) {
    return <p className="text-[10px] text-text-muted p-2">Loading files...</p>;
  }

  return (
    <div className="border-t border-border/30">
      {/* Header */}
      <div className="flex items-center justify-between px-3 py-1.5">
        <span className="text-[10px] text-text-muted uppercase tracking-wider">Files</span>
        <button
          onClick={() => setShowNewFile(!showNewFile)}
          className="text-[10px] text-primary hover:text-primary/80 flex items-center gap-0.5 transition-colors"
        >
          <Plus size={10} /> Add
        </button>
      </div>

      {/* New file input */}
      {showNewFile && (
        <div className="px-3 pb-2 flex items-center gap-1.5">
          <input
            value={newFilePath}
            onChange={(e) => setNewFilePath(e.target.value)}
            placeholder="scripts/my-script.sh"
            className="flex-1 bg-background/60 border border-border/40 rounded px-2 py-1 text-[10px] font-mono text-text-primary placeholder:text-text-muted/50 focus:outline-none focus:border-primary/50"
            onKeyDown={(e) => e.key === 'Enter' && handleCreateFile()}
          />
          <button
            onClick={handleCreateFile}
            className="px-2 py-1 text-[10px] text-primary bg-primary/10 rounded hover:bg-primary/20 transition-colors"
          >
            Create
          </button>
        </div>
      )}

      {/* File list */}
      {(files ?? []).length === 0 ? (
        <p className="text-[10px] text-text-muted/50 px-3 pb-2 italic">No supporting files</p>
      ) : (
        <div className="px-2 pb-2 space-y-0.5">
          {Object.entries(grouped).map(([dir, dirFiles]) => (
            <div key={dir}>
              <button
                onClick={() => toggleDir(dir)}
                className="w-full flex items-center gap-1.5 px-1 py-0.5 text-[10px] text-text-secondary hover:text-text-primary transition-colors"
              >
                {expandedDirs.has(dir) ? <ChevronDown size={10} /> : <ChevronRight size={10} />}
                {expandedDirs.has(dir) ? <FolderOpen size={10} className="text-primary/50" /> : <Folder size={10} className="text-text-muted" />}
                <span className="font-mono">{dir}/</span>
                <span className="text-text-muted">({dirFiles.length})</span>
              </button>
              {expandedDirs.has(dir) && (
                <div className="ml-4 space-y-0.5">
                  {dirFiles.map((file) => (
                    <div
                      key={file.path}
                      className="flex items-center gap-1.5 px-1 py-0.5 rounded hover:bg-surface-highlight/50 group"
                    >
                      <File size={9} className="text-text-muted flex-shrink-0" />
                      <button
                        onClick={() => onSelectFile?.(file.path, '')}
                        className="text-[10px] font-mono text-text-secondary hover:text-primary flex-1 text-left truncate transition-colors"
                      >
                        {file.path.split('/').pop()}
                      </button>
                      <span className="text-[9px] text-text-muted font-mono">
                        {formatFileSize(file.size)}
                      </span>
                      <button
                        onClick={() => handleDeleteFile(file.path)}
                        className="opacity-0 group-hover:opacity-100 p-0.5 text-text-muted hover:text-status-error transition-all"
                      >
                        <Trash2 size={9} />
                      </button>
                    </div>
                  ))}
                </div>
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

// Group files by their top-level directory
function groupFilesByDir(files: SkillFile[]): Record<string, SkillFile[]> {
  const grouped: Record<string, SkillFile[]> = {};
  for (const file of files ?? []) {
    if (file.isDir) continue;
    const parts = file.path.split('/');
    const dir = parts.length > 1 ? parts[0] : '_root';
    if (!grouped[dir]) grouped[dir] = [];
    grouped[dir].push(file);
  }
  return grouped;
}

function formatFileSize(bytes: number): string {
  if (bytes < 1024) return `${bytes}B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)}K`;
  return `${(bytes / (1024 * 1024)).toFixed(1)}M`;
}
