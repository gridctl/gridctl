import { useState, useEffect, useMemo } from 'react';
import { BookOpen, ChevronRight, Layers, Code, Database, Activity, Bot } from 'lucide-react';
import type { LucideIcon } from 'lucide-react';
import { cn } from '../../lib/cn';
import { fetchStackRecipes, type StackRecipe } from '../../lib/api';

interface RecipePickerProps {
  onSelect: (spec: string) => void;
  onClose: () => void;
}

const CATEGORY_META: Record<string, { icon: LucideIcon; label: string; color: string }> = {
  ai: { icon: Bot, label: 'AI & Agents', color: 'text-tertiary' },
  development: { icon: Code, label: 'Development', color: 'text-primary' },
  data: { icon: Database, label: 'Data & Analytics', color: 'text-secondary' },
  operations: { icon: Activity, label: 'Operations', color: 'text-blue-400' },
};

/**
 * Recipe picker — shows pre-built stack templates with YAML previews.
 * Available from the wizard and canvas empty state.
 */
export function RecipePicker({ onSelect, onClose }: RecipePickerProps) {
  const [recipes, setRecipes] = useState<StackRecipe[]>([]);
  const [loading, setLoading] = useState(true);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [filter, setFilter] = useState<string | null>(null);

  useEffect(() => {
    fetchStackRecipes()
      .then((r) => {
        setRecipes(r);
        setLoading(false);
      })
      .catch(() => setLoading(false));
  }, []);

  const categories = useMemo(() => {
    const cats = new Set(recipes.map((r) => r.category));
    return Array.from(cats);
  }, [recipes]);

  const filtered = useMemo(() => {
    if (!filter) return recipes;
    return recipes.filter((r) => r.category === filter);
  }, [recipes, filter]);

  const selectedRecipe = recipes.find((r) => r.id === selectedId);

  if (loading) {
    return (
      <div className="flex items-center justify-center p-8">
        <span className="text-xs text-text-muted">Loading recipes...</span>
      </div>
    );
  }

  return (
    <div className="flex flex-col h-full">
      {/* Header */}
      <div className="flex items-center gap-2 px-4 py-3 border-b border-border/20">
        <BookOpen size={14} className="text-primary" />
        <span className="text-xs font-medium text-text-primary">Stack Recipes</span>
        <span className="text-[10px] text-text-muted ml-auto">{recipes.length} templates</span>
      </div>

      {/* Category filters */}
      <div className="flex items-center gap-1.5 px-4 py-2 border-b border-border/10">
        <button
          onClick={() => setFilter(null)}
          className={cn(
            'px-2 py-0.5 rounded text-[10px] transition-all',
            !filter ? 'bg-primary/20 text-primary' : 'text-text-muted hover:text-text-primary',
          )}
        >
          All
        </button>
        {categories.map((cat) => {
          const meta = CATEGORY_META[cat] || { icon: Layers, label: cat, color: 'text-text-muted' };
          return (
            <button
              key={cat}
              onClick={() => setFilter(filter === cat ? null : cat)}
              className={cn(
                'flex items-center gap-1 px-2 py-0.5 rounded text-[10px] transition-all',
                filter === cat ? 'bg-primary/20 text-primary' : 'text-text-muted hover:text-text-primary',
              )}
            >
              <meta.icon size={10} />
              {meta.label}
            </button>
          );
        })}
      </div>

      <div className="flex flex-1 min-h-0">
        {/* Recipe list */}
        <div className="w-1/2 border-r border-border/10 overflow-y-auto">
          {filtered.map((recipe) => {
            const meta = CATEGORY_META[recipe.category] || { icon: Layers, label: recipe.category, color: 'text-text-muted' };
            const isSelected = selectedId === recipe.id;
            return (
              <button
                key={recipe.id}
                onClick={() => setSelectedId(recipe.id)}
                className={cn(
                  'w-full text-left px-4 py-3 border-b border-border/10 transition-all',
                  isSelected ? 'bg-primary/5 border-l-2 border-l-primary' : 'hover:bg-white/[0.02]',
                )}
              >
                <div className="flex items-center gap-2">
                  <meta.icon size={12} className={meta.color} />
                  <span className="text-xs font-medium text-text-primary">{recipe.name}</span>
                  <ChevronRight size={10} className="text-text-muted ml-auto" />
                </div>
                <p className="text-[10px] text-text-muted mt-1 leading-relaxed">{recipe.description}</p>
              </button>
            );
          })}
        </div>

        {/* Preview pane */}
        <div className="w-1/2 flex flex-col">
          {selectedRecipe ? (
            <>
              <div className="flex-1 overflow-y-auto p-4">
                <pre className="text-[10px] font-mono text-text-secondary whitespace-pre-wrap leading-relaxed">
                  {selectedRecipe.spec}
                </pre>
              </div>
              <div className="px-4 py-3 border-t border-border/20 flex items-center gap-2">
                <button
                  onClick={() => onSelect(selectedRecipe.spec)}
                  className={cn(
                    'flex-1 px-3 py-1.5 rounded-lg text-xs font-medium',
                    'bg-primary/20 text-primary hover:bg-primary/30 border border-primary/30',
                    'transition-all duration-200',
                  )}
                >
                  Use this recipe
                </button>
                <button
                  onClick={onClose}
                  className="px-3 py-1.5 rounded-lg text-xs text-text-muted hover:text-text-primary transition-all"
                >
                  Cancel
                </button>
              </div>
            </>
          ) : (
            <div className="flex items-center justify-center h-full">
              <div className="text-center">
                <Layers size={24} className="text-text-muted/30 mx-auto mb-2" />
                <p className="text-[10px] text-text-muted">Select a recipe to preview</p>
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
