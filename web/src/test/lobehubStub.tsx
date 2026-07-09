// Test stub for @lobehub/icons. Under vitest the package's barrel is
// externalized and transitively imports an @emoji-mart/data JSON module that
// Node ESM rejects without an import attribute, so we swap every icon for a
// minimal svg honoring the same size/className contract. Aliased in
// vitest.config.ts.
interface IconStubProps {
  size?: number;
  className?: string;
}

function makeIcon(name: string) {
  function IconStub({ size, className }: IconStubProps) {
    return <svg data-icon={name} width={size} height={size} className={className} />;
  }
  IconStub.displayName = name;
  return IconStub;
}

// Mirrors lobehub's CompoundedIcon shape: the base component IS the mono
// variant; there is no .Mono property.
function makeCompoundIcon(name: string) {
  return Object.assign(makeIcon(name), {
    Avatar: makeIcon(`${name}.Avatar`),
    Combine: makeIcon(`${name}.Combine`),
    Text: makeIcon(`${name}.Text`),
    colorPrimary: '#000',
    title: name,
  });
}

export const MCP = makeCompoundIcon('MCP');
export const Claude = makeCompoundIcon('Claude');
export const Cursor = makeCompoundIcon('Cursor');
export const Windsurf = makeCompoundIcon('Windsurf');
export const Gemini = makeCompoundIcon('Gemini');
export const Antigravity = makeCompoundIcon('Antigravity');
export const OpenCode = makeCompoundIcon('OpenCode');
export const Grok = makeCompoundIcon('Grok');
export const Cline = makeCompoundIcon('Cline');
export const RooCode = makeCompoundIcon('RooCode');
export const Goose = makeCompoundIcon('Goose');
export const Codex = makeCompoundIcon('Codex');
