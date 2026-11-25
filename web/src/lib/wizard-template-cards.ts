export interface Template {
  id: string;
  name: string;
  description: string;
  preview: string; // 3-line YAML snippet
}

export const templatesByType: Record<string, Template[]> = {
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
      description: 'Build from a Git repository or local path with a custom Dockerfile',
      preview: 'name: my-server\nsource:\n  type: git',
    },
    {
      id: 'python-package',
      name: 'Python Package',
      description: 'Build an exact PyPI release into an isolated container',
      preview: 'source:\n  type: pypi\n  package: my-server',
    },
  ],
  stack: [
    {
      id: 'blank',
      name: 'Blank Stack',
      description: 'Start with a minimal stack config',
      preview: 'version: "1"\nname: my-stack',
    },
    {
      id: 'basic',
      name: 'Basic Stack',
      description: 'Stack with one container server',
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
