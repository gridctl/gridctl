import { FileCode2, Check } from 'lucide-react';
import { cn } from '../../lib/cn';
import type { ResourceType } from '../../lib/yaml-builder';

interface Template {
  id: string;
  name: string;
  description: string;
  preview: string; // 3-line YAML snippet
}

const templatesByType: Record<string, Template[]> = {
  'mcp-server': [
    {
      id: 'blank',
      name: 'Blank Server',
      description: 'Start with an empty MCP server config',
      preview: 'name: my-server\nimage: ""\ntransport: http',
    },
    {
      id: 'container-http',
      name: 'Container (HTTP)',
      description: 'Docker container with HTTP transport',
      preview: 'name: my-server\nimage: my-image:latest\nport: 8080',
    },
    {
      id: 'container-stdio',
      name: 'Container (stdio)',
      description: 'Docker container with stdio transport',
      preview: 'name: my-server\nimage: my-image:latest\ntransport: stdio',
    },
    {
      id: 'external-url',
      name: 'External URL',
      description: 'Connect to a remote MCP server',
      preview: 'name: my-server\nurl: https://api.example.com\ntransport: sse',
    },
    {
      id: 'local-process',
      name: 'Local Process',
      description: 'Run a local command as an MCP server',
      preview: 'name: my-server\ncommand:\n  - npx my-server',
    },
    {
      id: 'from-source',
      name: 'Build from Source',
      description: 'Build from a Git repository',
      preview: 'name: my-server\nsource:\n  type: git',
    },
  ],
  stack: [
    {
      id: 'blank',
      name: 'Blank Stack',
      description: 'Start with a minimal stack config',
      preview: 'version: "1"\nname: my-stack\nnetwork:',
    },
    {
      id: 'basic',
      name: 'Basic Stack',
      description: 'Stack with one server and network',
      preview: 'version: "1"\nname: my-stack\nmcp-servers: [...]',
    },
  ],
  agent: [
    {
      id: 'blank',
      name: 'Blank Agent',
      description: 'Start with an empty agent config',
      preview: 'name: my-agent\nimage: ""\nuses: []',
    },
    {
      id: 'container',
      name: 'Container Agent',
      description: 'Docker-based agent with tool access',
      preview: 'name: my-agent\nimage: my-image:latest\nuses: [server]',
    },
    {
      id: 'headless',
      name: 'Headless Agent',
      description: 'Runtime-based agent with a prompt',
      preview: 'name: my-agent\nruntime: claude-code\nprompt: "..."',
    },
  ],
  resource: [
    {
      id: 'blank',
      name: 'Blank Resource',
      description: 'Start with an empty resource',
      preview: 'name: my-resource\nimage: ""\nenv: {}',
    },
    {
      id: 'postgres',
      name: 'PostgreSQL',
      description: 'PostgreSQL database with defaults',
      preview: 'name: postgres\nimage: postgres:16\nports: ["5432:5432"]',
    },
    {
      id: 'redis',
      name: 'Redis',
      description: 'Redis cache with defaults',
      preview: 'name: redis\nimage: redis:7-alpine\nports: ["6379:6379"]',
    },
    {
      id: 'mysql',
      name: 'MySQL',
      description: 'MySQL database with defaults',
      preview: 'name: mysql\nimage: mysql:8\nports: ["3306:3306"]',
    },
    {
      id: 'mongodb',
      name: 'MongoDB',
      description: 'MongoDB with defaults',
      preview: 'name: mongodb\nimage: mongo:7\nports: ["27017:27017"]',
    },
  ],
  skill: [],
  secret: [],
};

interface TemplateGridProps {
  resourceType: ResourceType;
  selected: string | null;
  onSelect: (templateId: string) => void;
}

export function TemplateGrid({ resourceType, selected, onSelect }: TemplateGridProps) {
  const templates = templatesByType[resourceType] || [];

  if (templates.length === 0) {
    return (
      <div className="flex items-center justify-center py-16 text-text-muted text-sm">
        No templates available for this type.
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center gap-2 mb-2">
        <FileCode2 size={14} className="text-primary" />
        <span className="text-xs font-medium text-text-secondary uppercase tracking-wider">
          Choose a template
        </span>
      </div>
      <div className="grid grid-cols-2 gap-3">
        {templates.map((template) => {
          const isSelected = selected === template.id;
          return (
            <button
              key={template.id}
              onClick={() => onSelect(template.id)}
              className={cn(
                'group relative text-left p-4 rounded-xl border transition-all duration-200',
                'bg-white/[0.03] hover:bg-white/[0.06]',
                isSelected
                  ? 'border-primary/50 shadow-[0_0_20px_rgba(245,158,11,0.1)]'
                  : 'border-white/[0.06] hover:border-white/[0.12]',
              )}
            >
              {isSelected && (
                <div className="absolute top-3 right-3">
                  <Check size={14} className="text-primary" />
                </div>
              )}
              <div className="text-sm font-medium text-text-primary mb-1">
                {template.name}
              </div>
              <div className="text-xs text-text-muted mb-3">
                {template.description}
              </div>
              <pre className="text-[10px] font-mono text-text-muted/70 leading-relaxed bg-background/40 rounded-lg px-2.5 py-2 border border-white/[0.04] overflow-hidden">
                {template.preview}
              </pre>
            </button>
          );
        })}
      </div>
    </div>
  );
}
