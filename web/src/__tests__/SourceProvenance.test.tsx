import { render, screen } from '@testing-library/react';
import '@testing-library/jest-dom';
import { describe, expect, it } from 'vitest';
import { SourceProvenance } from '../components/sidebar/SourceProvenance';

describe('SourceProvenance', () => {
  it('shows Python package identity, immutable provenance, and actual image', () => {
    render(
      <SourceProvenance
        kind="Python container"
        image="gridctl-demo-fetch:0.6.0-a1b2c3d4"
        source={{
          type: 'pypi',
          package: 'mcp-server-fetch',
          version: '0.6.0',
          artifact: 'fetch-0.6.0-py3-none-any.whl',
        }}
      />,
    );

    expect(screen.getByText('Python container')).toBeInTheDocument();
    expect(screen.getByText('mcp-server-fetch==0.6.0')).toBeInTheDocument();
    expect(screen.getByText('fetch-0.6.0-py3-none-any.whl')).toBeInTheDocument();
    expect(screen.getByText('gridctl-demo-fetch:0.6.0-a1b2c3d4')).toBeInTheDocument();
  });

  it('shows a Git ref and resolved commit', () => {
    render(
      <SourceProvenance
        kind="Python container"
        source={{
          type: 'git',
          url: 'https://github.com/example/server.git',
          ref: 'main',
          commit: '0123456789abcdef',
        }}
      />,
    );

    expect(screen.getByText('https://github.com/example/server.git#main')).toBeInTheDocument();
    expect(screen.getByText('0123456789abcdef')).toBeInTheDocument();
  });
});
